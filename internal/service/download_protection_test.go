package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDownloadProtectionThresholdAndPublicIP(t *testing.T) {
	for _, tc := range []struct{ size, want int64 }{{1, 2}, {10, 13}, {11, 15}, {8000000000, 10400000000}} {
		got, err := downloadThreshold(tc.size)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %d %v", tc, got, err)
		}
	}
	for _, size := range []int64{0, -1, 9223372036854775807} {
		if _, err := downloadThreshold(size); err == nil {
			t.Fatal("invalid threshold")
		}
	}
	for _, ip := range []string{"", "127.0.0.1", "10.0.0.1", "100.64.1.1", "192.0.2.1", "::1", "fe80::1", "2001:db8::1", "8.8.8.8/32", "8.8.8.8,1.1.1.1"} {
		if _, err := publicDownloadIP(ip); err == nil {
			t.Fatalf("accepted %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		if _, err := publicDownloadIP(ip); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDownloadProtectionSignedIPAndIssuancePersistence(t *testing.T) {
	expires := time.Now().Add(30 * time.Minute)
	recorded := false
	client := &http.Client{Transport: downloadRoundTripper(func(r *http.Request) (*http.Response, error) {
		var payload struct{ Policy string }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		var policy struct {
			Statement []struct {
				Condition map[string]map[string]string
				Resource  []string
			}
		}
		if err := json.Unmarshal([]byte(payload.Policy), &policy); err != nil {
			t.Fatal(err)
		}
		if len(policy.Statement) != 1 || policy.Statement[0].Condition["ip_equal"]["qcs:ip"] != "8.8.8.8" {
			t.Fatalf("missing IP constraint: %s", payload.Policy)
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	cfg := config.StorageConfig{S3Bucket: "bucket-123", S3Region: "ap-singapore", S3AccessKey: "fixture", S3SecretKey: "fixture"}
	ctx := context.WithValue(context.Background(), downloadSignContextKey{}, downloadSignContext{IP: "8.8.8.8", Record: func(id string, expiry time.Time) error { recorded = true; return nil }})
	// A malformed STS response cannot be recorded or delivered.
	if _, err := directCOSDownloadWithClient(ctx, cfg, "file.bam", "file.bam", expires, client); err == nil || recorded {
		t.Fatal("malformed response was issued")
	}
	client.Transport = downloadRoundTripper(func(*http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"Response":{"Credentials":{"TmpSecretId":"test-key-id","TmpSecretKey":"test-secret","Token":"test-token"},"ExpiredTime":%d}}`, expires.Add(30*time.Second).Unix())
		return &http.Response{StatusCode: 200, Body: ioBody(body), Header: make(http.Header)}, nil
	})
	link, err := directCOSDownloadWithClient(ctx, cfg, "file.bam", "file.bam", expires, client)
	if err != nil || !recorded || strings.Contains(link, "test-secret") {
		t.Fatalf("signing failed: %v", err)
	}
	ctx = context.WithValue(ctx, downloadSignContextKey{}, downloadSignContext{IP: "8.8.8.8", Record: func(string, time.Time) error { return errors.New("transaction_failed") }})
	if link, err := directCOSDownloadWithClient(ctx, cfg, "file.bam", "file.bam", expires, client); err == nil || link != "" {
		t.Fatal("unrecorded link escaped")
	}
}

func ioBody(s string) *testStringBody { return &testStringBody{Reader: strings.NewReader(s)} }

type testStringBody struct{ *strings.Reader }

func (*testStringBody) Close() error { return nil }

func TestDownloadProtectionParseCLSAndSigning(t *testing.T) {
	for _, raw := range []string{`{"bucketName":"b","userSecretKeyId":"k","requestId":"r","reqPath":"/f","reqMethod":"GET","resBytesSent":"13","eventTime":"2026-10-29T01:02:03Z"}`, `{"bucketName":"b","userSecretKeyId":"k","requestId":"r","reqPath":"/f","reqMethod":"GET","resBytesSent":13,"eventTime":1793235723}`} {
		e, err := parseDownloadLog(raw)
		if err != nil || e.Bytes != 13 {
			t.Fatalf("parse: %v", err)
		}
	}
	if _, err := parseDownloadLog(`{"resBytesSent":-1}`); err == nil {
		t.Fatal("negative bytes")
	}
	file := t.TempDir() + "/cls.json"
	if err := os.WriteFile(file, []byte(`{"secret_id":"fixture-id","secret_key":"fixture-secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.StorageConfig{DownloadCLSRegion: "ap-singapore", DownloadCLSTopicID: "topic", DownloadCLSCredentialsFile: file, S3Bucket: "bucket-123"}
	client := &http.Client{Transport: downloadRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "cls.tencentcloudapi.com" || req.Header.Get("X-TC-Action") != "SearchLog" || !strings.Contains(req.Header.Get("Authorization"), "Credential=fixture-id/") {
			t.Fatal("wrong CLS request")
		}
		var body map[string]interface{}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["TopicId"] != "topic" || body["Context"] != "cursor" || body["Limit"] != float64(100) {
			t.Fatal("bad page")
		}
		return &http.Response{StatusCode: 200, Body: ioBody(`{"Response":{"ListOver":false,"Context":"next","Results":[{"LogJson":"{}"}]}}`), Header: make(http.Header)}, nil
	})}
	page, err := clsSearch(context.Background(), cfg, time.Now().Add(-time.Hour), time.Now(), "cursor", client)
	if err != nil || page.Response.Context != "next" {
		t.Fatal(err)
	}
}

func protectionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("DOWNLOAD_PROTECTION_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated DOWNLOAD_PROTECTION_TEST_DSN")
	}
	if !strings.Contains(dsn, "host=127.0.0.1 ") || !strings.Contains(dsn, "dbname=download_protection") || strings.Contains(dsn, "search_path") {
		t.Fatal("only dedicated local download_protection database allowed")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := "download_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	old := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = old
		sql, _ := db.DB()
		sql.Close()
		base.Exec("DROP SCHEMA " + schema + " CASCADE")
		pool, _ := base.DB()
		pool.Close()
	})
	if err := db.AutoMigrate(&model.ResultDownload{}, &model.DownloadIssuance{}, &model.DownloadUsageEvent{}, &model.DownloadUsageSync{}, &model.DownloadNetworkRequest{}, &model.DownloadProtectionEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE tasks (id bigserial PRIMARY KEY,uuid text,external_org_id text,execution_attempt_id text,expires_at timestamptz,deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDownloadProtectionPostgresAtomicUsageAndRebind(t *testing.T) {
	db := protectionTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	until := now.Add(2 * time.Hour)
	if err := db.Create(&model.DownloadUsageSync{ID: "cos", LastSuccessAt: &now}).Error; err != nil {
		t.Fatal(err)
	}
	q := model.ResultDownload{ID: uuid.NewString(), TaskUUID: "task", AttemptID: "attempt", OrgID: "org", UserID: 7, Kind: "zip", ObjectKey: "file", ETag: `"etag"`, SizeBytes: 100, LinkExpiresAt: &until, ChargedAt: &now, ProtectionVersion: 1, BoundIP: "8.8.8.8", TrafficThresholdBytes: 130, IssuanceState: "active", QuoteExpiresAt: until}
	if err := db.Create(&q).Error; err != nil {
		t.Fatal(err)
	}
	issue := model.DownloadIssuance{ID: uuid.NewString(), GrantID: q.ID, KeyHash: downloadHash("key"), ObjectHash: downloadHash("/file"), Bucket: "bucket", ExpiresAt: now.Add(10 * time.Minute), CredentialExpiresAt: now.Add(11 * time.Minute)}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatal(err)
	}
	entry := DownloadLog{Bucket: "bucket", KeyID: "key", RequestID: "same-request", Path: "/file", Method: "GET", Bytes: 40, EventTime: now.Format(time.RFC3339Nano)}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- ingestDownloadLog(ctx, db, entry) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&q, "id = ?", q.ID).Error; err != nil {
		t.Fatal(err)
	}
	if q.ObservedBytes != 40 {
		t.Fatalf("duplicate counted %d", q.ObservedBytes)
	}
	invalid := entry
	invalid.RequestID = "wrong-object"
	invalid.Path = "/another"
	if err := ingestDownloadLog(ctx, db, invalid); err == nil {
		t.Fatal("wrong object accepted")
	}
	// A failed final grant update must roll back the receipt as well.
	if err := db.Exec(`CREATE FUNCTION reject_usage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.observed_bytes>50 THEN RAISE EXCEPTION 'test rollback'; END IF; RETURN NEW; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TRIGGER reject_usage BEFORE UPDATE ON result_downloads FOR EACH ROW EXECUTE FUNCTION reject_usage()`)
	more := entry
	more.RequestID = "retry-after-rollback"
	more.Bytes = 20
	if err := ingestDownloadLog(ctx, db, more); err == nil {
		t.Fatal("expected rollback")
	}
	var count int64
	db.Model(&model.DownloadUsageEvent{}).Where("id = ?", downloadHash("bucket\x00"+more.RequestID)).Count(&count)
	if count != 0 {
		t.Fatal("receipt survived rollback")
	}
	db.Exec(`DROP TRIGGER reject_usage ON result_downloads`)
	if err := ingestDownloadLog(ctx, db, more); err != nil {
		t.Fatal(err)
	}
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "HEAD" {
			t.Error("unexpected object transfer")
		}
		w.Header().Set("Content-Length", "100")
		w.Header().Set("ETag", `"etag"`)
	}))
	defer storage.Close()
	cfg := &config.Config{Edition: "saas", Storage: config.StorageConfig{DownloadProtectionEnabled: true, S3Endpoint: storage.URL, S3UsePathStyle: true, S3Bucket: "bucket", S3Region: "ap-singapore", S3AccessKey: "fixture", S3SecretKey: "fixture"}}
	svc := &ResultDownloadService{cfg: cfg}
	db.Exec(`INSERT INTO tasks(uuid,external_org_id,execution_attempt_id) VALUES ('task','org','attempt')`)
	task := &model.Task{UUID: "task", ExternalOrgID: "org", ExecutionAttemptID: "attempt"}
	actor := model.OverlayActor{UserID: 7, OrgID: "org"}
	if _, err := svc.RequestNetwork(ctx, task, model.OverlayActor{UserID: 8, OrgID: "other"}, q.ID, "1.1.1.1"); err == nil {
		t.Fatal("cross organization access")
	}
	request, err := svc.RequestNetwork(ctx, task, actor, q.ID, "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := svc.RequestNetwork(ctx, task, actor, q.ID, "1.1.1.1")
	if err != nil || repeat.ID != request.ID {
		t.Fatal("request not idempotent")
	}
	db.First(&q, "id = ?", q.ID)
	if err := svc.ManageProtection(ctx, q.ID, "approve", request.ID, "approved network change", q.Revision+1, 1); !errors.Is(err, ErrDownloadRevision) {
		t.Fatalf("missing conflict: %v", err)
	}
	if err := svc.ManageProtection(ctx, q.ID, "approve", request.ID, "approved network change", q.Revision, 1); err != nil {
		t.Fatal(err)
	}
	db.First(&q, "id = ?", q.ID)
	err = db.Transaction(func(tx *gorm.DB) error { return svc.protectIssue(tx, &q, "1.1.1.1", time.Now()) })
	if err == nil || err.Error() != "download_rebind_wait" {
		t.Fatal("early rebind")
	}
	past := now.Add(-time.Second)
	db.Model(&model.DownloadNetworkRequest{}).Where("id = ?", request.ID).Update("not_before", past)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := svc.protectIssue(tx, &q, "1.1.1.1", time.Now()); err != nil {
			return err
		}
		return tx.Save(&q).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.BoundIP != "1.1.1.1" || q.ObservedBytes != 60 || !q.LinkExpiresAt.Equal(until.Truncate(time.Microsecond)) {
		t.Fatalf("rebind changed paid grant: %+v", q)
	}
	more.RequestID = "threshold"
	more.Bytes = 70
	if err := ingestDownloadLog(ctx, db, more); err != nil {
		t.Fatal(err)
	}
	db.First(&q, "id = ?", q.ID)
	if q.ObservedBytes != 130 || q.IssuanceState != "threshold" {
		t.Fatal("threshold not latched")
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return svc.protectIssue(tx, &q, "1.1.1.1", time.Now()) }); err == nil || err.Error() != "download_traffic_threshold" {
		t.Fatal("threshold bypass")
	}
	fresh := q
	fresh.ID = uuid.NewString()
	fresh.ProtectionVersion = 0
	fresh.ChargedAt = nil
	fresh.LinkExpiresAt = nil
	fresh.IssuanceState = ""
	fresh.ObservedBytes = 0
	selected, err := reuseOrCreateDownloadQuote(ctx, task, &fresh)
	if err != nil || selected.ID == q.ID {
		t.Fatalf("cannot repurchase: %v", err)
	}
	// Restart/stale telemetry never falls back to an unbound URL.
	stale := now.Add(-6 * time.Minute)
	db.Model(&model.DownloadUsageSync{}).Where("id = ?", "cos").Update("last_success_at", stale)
	if err := usageHealthy(db, time.Now()); err == nil {
		t.Fatal("stale sync allowed")
	}
}

func TestDownloadProtectionPostgresLogPaginationRecovery(t *testing.T) {
	db := protectionTestDB(t)
	now := time.Now().UTC()
	cfg := &config.Config{Edition: "saas"}
	q := model.ResultDownload{ID: uuid.NewString(), SizeBytes: 100, ProtectionVersion: 1, TrafficThresholdBytes: 130, IssuanceState: "active"}
	db.Create(&q)
	issue := model.DownloadIssuance{ID: uuid.NewString(), GrantID: q.ID, KeyHash: downloadHash("key"), ObjectHash: downloadHash("/file"), Bucket: "bucket"}
	db.Create(&issue)
	failSecond := true
	reader := func(ctx context.Context, storage config.StorageConfig, from, to time.Time, cursor string, client *http.Client) (clsLogPage, error) {
		var page clsLogPage
		if cursor == "page2" && failSecond {
			return page, errors.New("provider secret diagnostics must not persist")
		}
		id := "page1"
		if cursor == "page2" {
			id = "page2"
			page.Response.ListOver = true
		} else {
			page.Response.Context = "page2"
		}
		record := fmt.Sprintf(`{"bucketName":"bucket","userSecretKeyId":"key","requestId":%q,"reqPath":"/file","reqMethod":"GET","resBytesSent":10,"eventTime":%q}`, id, now.Format(time.RFC3339Nano))
		page.Response.Results = append(page.Response.Results, struct{ LogJson string }{record})
		return page, nil
	}
	firstErr := syncDownloadUsage(context.Background(), cfg, reader)
	if firstErr == nil {
		t.Fatal("missing page failure")
	}
	var state model.DownloadUsageSync
	stateErr := db.First(&state, "id = ?", "cos").Error
	if state.LastSuccessAt != nil || !state.Cursor.IsZero() || state.LastError != "download_usage_sync_failed" {
		t.Fatalf("bad failed cursor: %+v; sync=%v state=%v", state, firstErr, stateErr)
	}
	db.First(&q, "id = ?", q.ID)
	if q.ObservedBytes != 10 {
		t.Fatal("first receipt missing")
	}
	failSecond = false
	if err := syncDownloadUsage(context.Background(), cfg, reader); err != nil {
		t.Fatal(err)
	}
	db.First(&q, "id = ?", q.ID)
	if q.ObservedBytes != 20 {
		t.Fatal("retry double counted")
	}
	db.First(&state, "id = ?", "cos")
	if state.LastSuccessAt == nil || state.LastError != "" || state.LastReplayAt == nil {
		t.Fatal("healthy cursor not persisted")
	}
	if err := syncDownloadUsage(context.Background(), cfg, reader); err != nil {
		t.Fatal(err)
	}
	db.First(&q, "id = ?", q.ID)
	if q.ObservedBytes != 20 {
		t.Fatal("restart replay double counted")
	}
}

func TestDownloadProtectionPostgresIssueFailClosedAndRefresh(t *testing.T) {
	db := protectionTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	db.Create(&model.DownloadUsageSync{ID: "cos", LastSuccessAt: &now})
	q := model.ResultDownload{ID: uuid.NewString(), TaskUUID: "task", AttemptID: "attempt", OrgID: "org", UserID: 7, Kind: "bam", ObjectKey: "file", Filename: "test.bam", ETag: `"etag"`, SizeBytes: 100, Credits: 1, QuoteExpiresAt: now.Add(10 * time.Minute)}
	if err := db.Create(&q).Error; err != nil {
		t.Fatal(err)
	}
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Header().Set("ETag", `"etag"`)
	}))
	defer storage.Close()
	charges := 0
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		charges++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"allowed":true,"credits_charged":1,"balance":99}`)
	}))
	defer billing.Close()
	cfg := &config.Config{Edition: "saas", Storage: config.StorageConfig{DownloadProtectionEnabled: true, S3Endpoint: storage.URL, S3UsePathStyle: true, S3Bucket: "bucket", S3Region: "ap-singapore", S3AccessKey: "fixture", S3SecretKey: "fixture"}}
	svc := &ResultDownloadService{cfg: cfg, overlay: NewOverlayClient(config.OverlayConfig{Enabled: true, BaseURL: billing.URL})}
	issued := 0
	svc.signDownload = func(c context.Context, _ config.StorageConfig, key, name string, expires time.Time) (string, error) {
		protection, ok := c.Value(downloadSignContextKey{}).(downloadSignContext)
		if !ok || protection.IP != "8.8.8.8" {
			t.Fatal("unbound signer")
		}
		issued++
		if err := protection.Record(fmt.Sprint("test-key-", issued), expires.Add(30*time.Second)); err != nil {
			return "", err
		}
		return "https://fixture.test/download", nil
	}
	task := &model.Task{UUID: "task", ExecutionAttemptID: "attempt", ExternalOrgID: "org"}
	actor := model.OverlayActor{UserID: 7, OrgID: "org"}
	if _, err := svc.Issue(ctx, task, actor, "127.0.0.1", q.ID); err == nil || charges != 0 {
		t.Fatal("invalid IP charged")
	}
	first, err := svc.Issue(ctx, task, actor, "8.8.8.8", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if charges != 1 || first["ip_bound"] != true {
		t.Fatal("missing binding or charge")
	}
	db.First(&q, "id = ?", q.ID)
	originalDeadline := *q.LinkExpiresAt
	if q.TrafficThresholdBytes != 130 || q.BoundIP != "8.8.8.8" {
		t.Fatal("wrong protection snapshot")
	}
	if _, err := svc.Issue(ctx, task, actor, "1.1.1.1", q.ID); err == nil || charges != 1 {
		t.Fatal("shared link issuance")
	}
	past := now.Add(-time.Minute)
	db.Model(&q).Update("last_issued_at", past)
	second, err := svc.Issue(ctx, task, actor, "8.8.8.8", q.ID)
	if err != nil || charges != 1 || second["ip_bound"] != true {
		t.Fatalf("refresh: %v", err)
	}
	db.First(&q, "id = ?", q.ID)
	if !q.LinkExpiresAt.Equal(originalDeadline) {
		t.Fatal("deadline extended")
	}
	var count int64
	db.Model(&model.DownloadIssuance{}).Where("grant_id = ?", q.ID).Count(&count)
	if count != 2 {
		t.Fatal("issuance not durable")
	}
	db.Model(&model.DownloadUsageSync{}).Where("id = ?", "cos").Update("last_success_at", now.Add(-6*time.Minute))
	if _, err := svc.Issue(ctx, task, actor, "8.8.8.8", q.ID); err == nil || err.Error() != "download_usage_unavailable" {
		t.Fatal("telemetry failure bypass")
	}
	if charges != 1 {
		t.Fatal("failure recharged")
	}
}
