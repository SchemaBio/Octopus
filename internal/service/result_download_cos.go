package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
)

// COS serves the exact object directly. The temporary secret key stays here;
// the URL includes only the temporary ID/token and a short-lived signature.
func directCOSDownload(ctx context.Context, cfg config.StorageConfig, key, filename string, expires time.Time) (string, error) {
	return directCOSDownloadWithClient(ctx, cfg, key, filename, expires, &http.Client{Timeout: 20 * time.Second})
}

func directCOSDownloadWithClient(ctx context.Context, cfg config.StorageConfig, key, filename string, expires time.Time, client *http.Client) (string, error) {
	cfg = downloadSettings(cfg)
	if cfg.S3AccessKey == "" || cfg.S3SecretKey == "" || strings.ContainsAny(key, "*?") || cfg.ResultDownloadTrafficLimit < 819200 || cfg.ResultDownloadTrafficLimit > 838860800 {
		return "", fmt.Errorf("COS download configuration is unavailable")
	}
	split := strings.LastIndex(cfg.S3Bucket, "-")
	if split < 1 || cfg.S3Region == "" {
		return "", fmt.Errorf("COS bucket/region is unavailable")
	}
	appID := cfg.S3Bucket[split+1:]
	if _, err := strconv.ParseUint(appID, 10, 64); err != nil {
		return "", fmt.Errorf("invalid COS bucket AppId")
	}
	policy, err := json.Marshal(map[string]interface{}{"version": "2.0", "statement": []interface{}{map[string]interface{}{
		"effect": "allow", "action": []string{"name/cos:GetObject"},
		"resource": []string{"qcs::cos:" + cfg.S3Region + ":uid/" + appID + ":" + cfg.S3Bucket + "/" + key},
	}}})
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	remaining := int64(expires.Sub(now).Seconds())
	if remaining <= 0 || remaining > int64(cfg.ResultDownloadLinkTTL.Seconds()) {
		return "", fmt.Errorf("download authorization expired")
	}
	payload, _ := json.Marshal(map[string]interface{}{"Name": "result-download", "DurationSeconds": remaining + 30, "Policy": string(policy)})
	hash := func(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
	mac := func(key []byte, data string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(data))
		return m.Sum(nil)
	}
	date := now.Format("2006-01-02")
	scope := date + "/sts/tc3_request"
	canonical := "POST\n/\n\ncontent-type:application/json\nhost:sts.tencentcloudapi.com\n\ncontent-type;host\n" + hash(payload)
	toSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(now.Unix(), 10) + "\n" + scope + "\n" + hash([]byte(canonical))
	signingKey := mac(mac(mac([]byte("TC3"+cfg.S3SecretKey), date), "sts"), "tc3_request")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://sts.tencentcloudapi.com", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TC-Action", "GetFederationToken")
	req.Header.Set("X-TC-Version", "2018-08-13")
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(now.Unix(), 10))
	req.Header.Set("X-TC-Region", cfg.S3Region)
	if cfg.S3SessionToken != "" {
		req.Header.Set("X-TC-Token", cfg.S3SessionToken)
	}
	req.Header.Set("Authorization", "TC3-HMAC-SHA256 Credential="+cfg.S3AccessKey+"/"+scope+", SignedHeaders=content-type;host, Signature="+hex.EncodeToString(mac(signingKey, toSign)))
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("COS temporary authorization unavailable")
	}
	defer resp.Body.Close()
	var envelope struct {
		Response struct {
			Credentials struct {
				ID    string `json:"TmpSecretId"`
				Key   string `json:"TmpSecretKey"`
				Token string `json:"Token"`
			} `json:"Credentials"`
			ExpiredTime int64 `json:"ExpiredTime"`
			Error       *struct {
				Code string `json:"Code"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope) != nil || resp.StatusCode != 200 {
		return "", fmt.Errorf("invalid COS temporary authorization response")
	}
	r := envelope.Response
	if r.Error != nil {
		return "", fmt.Errorf("COS temporary authorization rejected (%s)", r.Error.Code)
	}
	if r.Credentials.ID == "" || r.Credentials.Key == "" || r.Credentials.Token == "" || r.ExpiredTime < expires.Unix() {
		return "", fmt.Errorf("COS temporary authorization does not cover requested validity")
	}
	host := cfg.S3Bucket + ".cos." + cfg.S3Region + ".myqcloud.com"
	u := &url.URL{Scheme: "https", Host: host, Path: "/" + key}
	encode := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	params := url.Values{"response-content-disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": filename})}, "x-cos-security-token": {r.Credentials.Token}}
	params.Set("x-cos-traffic-limit", strconv.FormatInt(cfg.ResultDownloadTrafficLimit, 10))
	query := "response-content-disposition=" + encode(params.Get("response-content-disposition")) + "&x-cos-security-token=" + encode(r.Credentials.Token) + "&x-cos-traffic-limit=" + encode(params.Get("x-cos-traffic-limit"))
	keyTime := strconv.FormatInt(now.Unix()-30, 10) + ";" + strconv.FormatInt(expires.Unix(), 10)
	sha := func(s string) string { h := sha1.Sum([]byte(s)); return hex.EncodeToString(h[:]) }
	shaMAC := func(key, s string) string {
		m := hmac.New(sha1.New, []byte(key))
		m.Write([]byte(s))
		return hex.EncodeToString(m.Sum(nil))
	}
	// COS signs the decoded request path; query/header values are RFC3986 encoded.
	httpString := "get\n" + u.Path + "\n" + query + "\nhost=" + encode(host) + "\n"
	signKey := shaMAC(r.Credentials.Key, keyTime)
	signature := shaMAC(signKey, "sha1\n"+keyTime+"\n"+sha(httpString)+"\n")
	params.Set("q-sign-algorithm", "sha1")
	params.Set("q-ak", r.Credentials.ID)
	params.Set("q-sign-time", keyTime)
	params.Set("q-key-time", keyTime)
	params.Set("q-header-list", "host")
	params.Set("q-url-param-list", "response-content-disposition;x-cos-security-token;x-cos-traffic-limit")
	params.Set("q-signature", signature)
	u.RawQuery = params.Encode()
	return u.String(), nil
}
