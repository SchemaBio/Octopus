package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net/netip"
	"net/url"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func MigrateDownloadProtection(cfg *config.Config) error {
	if !cfg.IsSaaS() {
		return nil
	}
	return database.DB.AutoMigrate(&model.DownloadIssuance{}, &model.DownloadUsageEvent{}, &model.DownloadUsageSync{}, &model.DownloadNetworkRequest{}, &model.DownloadProtectionEvent{})
}

func downloadHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

// ceil(size*13/10), without an overflowing intermediate multiplication.
func downloadThreshold(size int64) (int64, error) {
	if size <= 0 || size > math.MaxInt64/13*10 {
		return 0, errors.New("download_invalid_size")
	}
	return size/10*13 + (size%10*13+9)/10, nil
}

func publicDownloadIP(value string) (string, error) {
	ip, err := netip.ParseAddr(value)
	if err != nil || ip.Zone() != "" {
		return "", errors.New("download_ip_unavailable")
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", errors.New("download_ip_unavailable")
	}
	for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return "", errors.New("download_ip_unavailable")
		}
	}
	return ip.String(), nil
}

func (s *ResultDownloadService) protectionEnabled() bool {
	return s.cfg.IsSaaS() && s.cfg.Storage.DownloadProtectionEnabled
}

func usageHealthy(db *gorm.DB, now time.Time) error {
	var sync model.DownloadUsageSync
	if db.First(&sync, "id = ?", "cos").Error != nil || sync.LastSuccessAt == nil || now.Sub(*sync.LastSuccessAt) > 5*time.Minute || sync.LastSuccessAt.After(now.Add(time.Minute)) {
		return errors.New("download_usage_unavailable")
	}
	return nil
}

func protectionEvent(tx *gorm.DB, grantID, action string, actor uint, reason string) error {
	return tx.Create(&model.DownloadProtectionEvent{ID: uuid.NewString(), GrantID: grantID, Action: action, Actor: actor, Reason: reason}).Error
}

// Called while holding the paid-grant row lock. Refresh never resets the quota.
func (s *ResultDownloadService) protectIssue(tx *gorm.DB, q *model.ResultDownload, ip string, now time.Time) error {
	if !s.protectionEnabled() && q.ProtectionVersion == 0 {
		return nil
	}
	if q.ProtectionVersion == 0 && q.ChargedAt != nil {
		return errors.New("download_legacy_grant")
	}
	if err := usageHealthy(tx, now); err != nil {
		return err
	}
	addr, err := publicDownloadIP(ip)
	if err != nil {
		return err
	}
	if q.ProtectionVersion == 0 {
		limit, err := downloadThreshold(q.SizeBytes)
		if err != nil {
			return err
		}
		q.ProtectionVersion = 1
		q.BoundIP = addr
		q.TrafficThresholdBytes = limit
		q.IssuanceState = "active"
	}
	// Before any successful issuance no URL has been delivered, so a failed
	// first attempt must not strand an unpaid quote on its initial network.
	if q.ChargedAt == nil && q.IssueCount == 0 {
		q.BoundIP = addr
	}
	if q.IssuanceState == "rebind_wait" {
		var pending model.DownloadNetworkRequest
		if err := tx.Where("grant_id = ? AND status = ?", q.ID, "approved_wait").First(&pending).Error; err != nil {
			return errors.New("download_rebind_wait")
		}
		if pending.NotBefore == nil || now.Before(*pending.NotBefore) {
			return errors.New("download_rebind_wait")
		}
		q.BoundIP = pending.TargetIP
		q.IssuanceState = "active"
		q.Revision++
		pending.Status = "applied"
		pending.Revision++
		if err := tx.Save(&pending).Error; err != nil {
			return err
		}
		if err := protectionEvent(tx, q.ID, "network_applied", 0, ""); err != nil {
			return err
		}
	}
	if q.ObservedBytes >= q.TrafficThresholdBytes || q.IssuanceState == "threshold" {
		return errors.New("download_traffic_threshold")
	}
	if q.IssuanceState != "active" {
		return errors.New("download_issuance_paused")
	}
	if q.BoundIP != addr {
		return errors.New("download_ip_mismatch")
	}
	return nil
}

type DownloadLog struct {
	Bucket    string `json:"bucketName"`
	KeyID     string `json:"userSecretKeyId"`
	RequestID string `json:"requestId"`
	Path      string `json:"reqPath"`
	Method    string `json:"reqMethod"`
	Bytes     int64  `json:"resBytesSent,string"`
	EventTime string `json:"eventTime"`
}

// One log ID, one increment. Late logs remain attributable after a grant expires.
func ingestDownloadLog(ctx context.Context, db *gorm.DB, entry DownloadLog) error {
	if entry.Method != "GET" && entry.Method != "get" {
		return nil
	}
	if entry.KeyID == "" || entry.RequestID == "" || entry.Bytes < 0 {
		return errors.New("download_log_invalid")
	}
	at, err := time.Parse(time.RFC3339Nano, entry.EventTime)
	if err != nil {
		return errors.New("download_log_invalid")
	}
	var issuance model.DownloadIssuance
	err = db.WithContext(ctx).Where("key_hash = ?", downloadHash(entry.KeyID)).First(&issuance).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	objectMatches := issuance.ObjectHash == downloadHash(entry.Path)
	if !objectMatches {
		decoded, err := url.PathUnescape(entry.Path)
		objectMatches = err == nil && issuance.ObjectHash == downloadHash(decoded)
	}
	if issuance.Bucket != entry.Bucket || !objectMatches {
		return errors.New("download_log_object_mismatch")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var q model.ResultDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&q, "id = ?", issuance.GrantID).Error; err != nil {
			return err
		}
		log := model.DownloadUsageEvent{ID: downloadHash(entry.Bucket + "\x00" + entry.RequestID), GrantID: q.ID, Bytes: entry.Bytes, EventTime: at}
		inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&log)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			var previous model.DownloadUsageEvent
			if err := tx.First(&previous, "id = ?", log.ID).Error; err != nil {
				return err
			}
			if previous.GrantID != q.ID || previous.Bytes != entry.Bytes {
				return errors.New("download_log_duplicate_conflict")
			}
			return nil
		}
		if entry.Bytes > math.MaxInt64-q.ObservedBytes {
			return errors.New("download_log_overflow")
		}
		q.ObservedBytes += entry.Bytes
		now := time.Now().UTC()
		q.UsageSyncedAt = &now
		q.Revision++
		if q.ProtectionVersion > 0 && q.ObservedBytes >= q.TrafficThresholdBytes && q.IssuanceState != "threshold" {
			q.IssuanceState = "threshold"
			if err := protectionEvent(tx, q.ID, "traffic_threshold", 0, ""); err != nil {
				return err
			}
		}
		return tx.Save(&q).Error
	})
}

func (s *ResultDownloadService) RequestNetwork(ctx context.Context, task *model.Task, actor model.OverlayActor, id, ip string) (*model.DownloadNetworkRequest, error) {
	target, err := publicDownloadIP(ip)
	if err != nil {
		return nil, err
	}
	var request model.DownloadNetworkRequest
	err = database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var q model.ResultDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_uuid = ? AND attempt_id = ? AND user_id = ? AND org_id = ?", id, task.UUID, task.ExecutionAttemptID, actor.UserID, actor.OrgID).First(&q).Error; err != nil {
			return errors.New("download_grant_not_found")
		}
		if q.ProtectionVersion == 0 || q.ChargedAt == nil || q.RefundedAt != nil || q.LinkExpiresAt == nil || !time.Now().Before(*q.LinkExpiresAt) {
			return errors.New("download_grant_unavailable")
		}
		if q.BoundIP == target {
			return errors.New("download_ip_unchanged")
		}
		if q.IssuanceState != "active" || q.ObservedBytes >= q.TrafficThresholdBytes {
			return errors.New("download_issuance_paused")
		}
		err := tx.Where("grant_id = ? AND status IN ?", q.ID, []string{"pending", "approved_wait"}).First(&request).Error
		if err == nil {
			if request.TargetIP != target {
				return errors.New("download_network_pending")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		request = model.DownloadNetworkRequest{ID: uuid.NewString(), GrantID: q.ID, TargetIP: target, Status: "pending"}
		if err := tx.Create(&request).Error; err != nil {
			return err
		}
		return protectionEvent(tx, q.ID, "network_requested", actor.UserID, "")
	})
	return &request, err
}
