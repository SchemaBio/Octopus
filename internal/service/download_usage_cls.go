package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

type clsCredentials struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
	Token     string `json:"token"`
}
type clsLogPage struct {
	Response struct {
		Context  string
		ListOver bool
		Analysis bool
		Results  []struct{ LogJson string }
		Error    *struct{ Code string }
	}
}

// Fixed endpoint and bounded responses: provider errors never expose credentials or raw logs.
func clsSearch(ctx context.Context, cfg config.StorageConfig, from, to time.Time, cursor string, client *http.Client) (clsLogPage, error) {
	var result clsLogPage
	if !regexp.MustCompile(`^[a-z]{2}-[a-z]+(?:-[0-9]+)?$`).MatchString(cfg.DownloadCLSRegion) || cfg.DownloadCLSTopicID == "" || cfg.S3Bucket == "" {
		return result, errors.New("download_cls_not_configured")
	}
	info, err := os.Stat(cfg.DownloadCLSCredentialsFile)
	if err != nil || info.Size() > 16384 {
		return result, errors.New("download_cls_credentials_unavailable")
	}
	raw, err := os.ReadFile(cfg.DownloadCLSCredentialsFile)
	if err != nil {
		return result, errors.New("download_cls_credentials_unavailable")
	}
	var credentials clsCredentials
	if json.Unmarshal(raw, &credentials) != nil || credentials.SecretID == "" || credentials.SecretKey == "" {
		return result, errors.New("download_cls_credentials_unavailable")
	}
	query := "bucketName:" + strconv.Quote(cfg.S3Bucket) + " AND (reqMethod:GET OR reqMethod:get)"
	body, _ := json.Marshal(map[string]interface{}{"TopicId": cfg.DownloadCLSTopicID, "From": from.UnixMilli(), "To": to.UnixMilli(), "Query": query, "Limit": 100, "Sort": "asc", "Context": cursor})
	host := "cls.tencentcloudapi.com"
	now := time.Now().UTC()
	date := now.Format("2006-01-02")
	scope := date + "/cls/tc3_request"
	hash := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	mac := func(key []byte, value string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(value))
		return h.Sum(nil)
	}
	canonical := "POST\n/\n\ncontent-type:application/json\nhost:" + host + "\n\ncontent-type;host\n" + hash(body)
	stringToSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(now.Unix(), 10) + "\n" + scope + "\n" + hash([]byte(canonical))
	key := mac(mac(mac([]byte("TC3"+credentials.SecretKey), date), "cls"), "tc3_request")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TC-Action", "SearchLog")
	req.Header.Set("X-TC-Version", "2020-10-16")
	req.Header.Set("X-TC-Region", cfg.DownloadCLSRegion)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(now.Unix(), 10))
	req.Header.Set("Authorization", "TC3-HMAC-SHA256 Credential="+credentials.SecretID+"/"+scope+", SignedHeaders=content-type;host, Signature="+hex.EncodeToString(mac(key, stringToSign)))
	if credentials.Token != "" {
		req.Header.Set("X-TC-Token", credentials.Token)
	}
	response, err := client.Do(req)
	if err != nil {
		return result, errors.New("download_cls_query_failed")
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode != 200 || decoder.Decode(&result) != nil || result.Response.Error != nil || result.Response.Analysis {
		return result, errors.New("download_cls_query_failed")
	}
	return result, nil
}

func parseDownloadLog(raw string) (DownloadLog, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil {
		return DownloadLog{}, errors.New("download_log_invalid")
	}
	text := func(key string) string {
		var value string
		if json.Unmarshal(fields[key], &value) == nil {
			return value
		}
		return string(fields[key])
	}
	// Other tenants/service calls in this bucket are not paid download events.
	e := DownloadLog{Bucket: text("bucketName"), KeyID: text("userSecretKeyId"), RequestID: text("requestId"), Path: text("reqPath"), Method: text("reqMethod"), EventTime: text("eventTime")}
	n, err := strconv.ParseInt(text("resBytesSent"), 10, 64)
	if err != nil || n < 0 {
		return e, errors.New("download_log_invalid")
	}
	e.Bytes = n
	if _, err := time.Parse(time.RFC3339Nano, e.EventTime); err != nil {
		stamp, parseErr := strconv.ParseInt(e.EventTime, 10, 64)
		if parseErr != nil {
			return e, errors.New("download_log_invalid")
		}
		if stamp > 1e12 {
			e.EventTime = time.UnixMilli(stamp).UTC().Format(time.RFC3339Nano)
		} else {
			e.EventTime = time.Unix(stamp, 0).UTC().Format(time.RFC3339Nano)
		}
	}
	if !strings.HasPrefix(e.Path, "/") {
		return e, errors.New("download_log_invalid")
	}
	return e, nil
}

func SyncDownloadUsage(ctx context.Context, cfg *config.Config) error {
	return syncDownloadUsage(ctx, cfg, clsSearch)
}

func syncDownloadUsage(ctx context.Context, cfg *config.Config, search func(context.Context, config.StorageConfig, time.Time, time.Time, string, *http.Client) (clsLogPage, error)) error {
	if !cfg.IsSaaS() {
		return errors.New("download_saas_only")
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	return database.DB.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		// Connection gives an initialized handle. Each operation needs a fresh
		// statement so ErrRecordNotFound and WHERE clauses cannot poison writes.
		conn = conn.Session(&gorm.Session{NewDB: true})
		var acquired bool
		if err := conn.Raw("SELECT pg_try_advisory_lock(831762901)").Scan(&acquired).Error; err != nil {
			return err
		}
		if !acquired {
			return errors.New("download_sync_busy")
		}
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn.WithContext(unlockCtx).Exec("SELECT pg_advisory_unlock(831762901)")
		}()
		var state model.DownloadUsageSync
		err := conn.First(&state, "id = ?", "cos").Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			state.ID = "cos"
			if err = conn.Create(&state).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		now := time.Now().UTC()
		from := state.Cursor.Add(-10 * time.Minute)
		replay := state.LastReplayAt == nil || now.Sub(*state.LastReplayAt) >= time.Hour
		if replay || state.Cursor.IsZero() {
			from = now.Add(-24 * time.Hour)
		}
		// A failed pagination never advances the durable cursor; replay deduplicates.
		client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		runErr := func() error {
			{
				token := ""
				for page := 0; ; page++ {
					if page >= 1000 {
						return errors.New("download_cls_backlog")
					}
					response, err := search(ctx, cfg.Storage, from, now, token, client)
					if err != nil {
						return err
					}
					for _, row := range response.Response.Results {
						entry, err := parseDownloadLog(row.LogJson)
						if err != nil {
							return err
						}
						// Anonymous GETs do not have a temporary key; never attribute them to a paid grant.
						if entry.KeyID == "" || entry.KeyID == "-" {
							continue
						}
						if err := ingestDownloadLog(ctx, database.DB, entry); err != nil {
							return err
						}
					}
					if response.Response.ListOver {
						break
					}
					next := response.Response.Context
					if next == "" || next == token {
						return errors.New("download_cls_pagination_failed")
					}
					token = next
				}
			}
			return nil
		}()
		if runErr != nil {
			// Only fixed categories; never persist provider text, object paths or URLs.
			category := "download_usage_sync_failed"
			if strings.HasPrefix(runErr.Error(), "download_") {
				category = runErr.Error()
			}
			errorCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			saveErr := conn.WithContext(errorCtx).Model(&state).Update("last_error", category).Error
			if saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("%s", category)
		}
		state.Cursor = now
		state.LastSuccessAt = &now
		state.LastError = ""
		if replay {
			state.LastReplayAt = &now
		}
		return conn.Save(&state).Error
	})
}

func StartDownloadUsageSync(ctx context.Context, cfg *config.Config) {
	if !cfg.IsSaaS() || cfg.Storage.DownloadCLSTopicID == "" {
		return
	}
	go func() {
		timer := time.NewTicker(time.Minute)
		defer timer.Stop()
		for {
			_ = SyncDownloadUsage(ctx, cfg)
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}
