package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/exampledata"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/writer"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test accepts only the explicitly named local throwaway database and a
// loopback S3 endpoint. It never reads the application/production DB_DSN.
func TestExampleIntegrationCopyPublicationAndExpiry(t *testing.T) {
	dsn := os.Getenv("EXAMPLE_DATA_TEST_DSN")
	endpoint := os.Getenv("EXAMPLE_DATA_TEST_S3")
	if dsn == "" || endpoint == "" {
		t.Skip("isolated PostgreSQL and S3 endpoints required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() != "127.0.0.1" || !strings.Contains(dsn, "host=127.0.0.1 ") || !strings.Contains(dsn, "dbname=example_test_local ") {
		t.Fatal("refusing non-test endpoints")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := "example_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = base.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		sqlDB, _ := base.DB()
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Error)})
	if err != nil {
		t.Fatal(err)
	}
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := database.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cfg := &config.Config{Edition: "saas", RetentionCleanupEnabled: true, RetentionBackupPolicyVerified: true}
	cfg.Storage = config.StorageConfig{Provider: "s3", S3Endpoint: endpoint, S3Region: "us-east-1", S3Bucket: "example-" + uuid.NewString(), S3AccessKey: "example_test_user", S3SecretKey: "example-test-password-only", S3UsePathStyle: true, RetentionDays: 7, BAMRetentionDays: 7, BAMCleanupEnabled: true, TaskRetentionEnabled: true}
	cfg.ResultQuery.CacheDir = t.TempDir()
	if err := MigrateExampleData(cfg); err != nil {
		t.Fatal(err)
	}
	if err := database.EnableSaaSRetention(); err != nil {
		t.Fatal(err)
	}
	store, err := newS3Storage(ctx, cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(store.bucket)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(store.bucket), VersioningConfiguration: &s3types.VersioningConfiguration{Status: s3types.BucketVersioningStatusEnabled}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	m := exampledata.Manifest{Version: "integration-v1", SampleID: "EXAMPLE", ReferenceGenome: "hg38", Pipeline: "single", PipelineVersion: "1", Sample: exampledata.Sample{Gender: "unknown", SampleType: "DNA", HPOTerms: json.RawMessage("[]")}}
	add := func(name string, role exampledata.Role, data []byte, compress bool) {
		t.Helper()
		if compress {
			var b bytes.Buffer
			g := gzip.NewWriter(&b)
			_, _ = g.Write(data)
			_ = g.Close()
			data = b.Bytes()
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".parquet") {
			f, err := local.NewLocalFileWriter(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			type row struct {
				Position int64 `parquet:"name=Position, type=INT64"`
			}
			w, err := writer.NewParquetWriter(f, new(row), 1)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Write(row{Position: 1}); err != nil {
				t.Fatal(err)
			}
			if err := w.WriteStop(); err != nil {
				t.Fatal(err)
			}
			f.Close()
			data, err = os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
		}
		h := sha256.Sum256(data)
		m.Objects = append(m.Objects, exampledata.Object{Path: name, Role: role, Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])})
	}
	add("example_R1.fastq.gz", exampledata.Read1, []byte("@read/1\nACGT\n+\n!!!!\n"), true)
	add("example_R2.fastq.gz", exampledata.Read2, []byte("@read/2\nACGT\n+\n!!!!\n"), true)
	add("example.bam", exampledata.BAM, []byte{'B', 'A', 'M', 1, 0, 0, 0, 0, 1, 0, 0, 0, 5, 0, 0, 0, 'c', 'h', 'r', '1', 0, 100, 0, 0, 0}, true)
	add("example.bam.bai", exampledata.BAMIndex, []byte{'B', 'A', 'I', 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false)
	add("example.snv_indel.parquet", exampledata.Result, []byte("PAR1"), false)
	m.Outputs = json.RawMessage(`{"Single.summary":{"qc_result":{"sample_id":"EXAMPLE"},"bam":"example://objects/example.bam","bai":"example://objects/example.bam.bai","snp_indel":"example://objects/example.snv_indel.parquet"}}`)
	if err := PublishExampleTemplate(ctx, cfg, root, m, true); err != nil {
		t.Fatal(err)
	}
	if err := PublishExampleTemplate(ctx, cfg, root, m, true); err == nil {
		t.Fatal("immutable version overwritten")
	}
	approved := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	in := model.ExampleDataGrant{ID: uuid.NewString(), OrgID: uuid.NewString(), UserID: 42, TemplateVersion: m.Version, ApprovedAt: approved}
	svc := NewExampleDataService(cfg)
	g, err := svc.Enqueue(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Enqueue(ctx, in)
	if err != nil || again.TaskUUID != g.TaskUUID {
		t.Fatal("non-idempotent grant", err)
	}
	var active atomic.Bool
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/overlay/example-data/eligibility" {
			t.Errorf("unexpected compute/billing call: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("test-only-secret"))
		mac.Write([]byte(r.Header.Get("X-Overlay-Timestamp") + "."))
		mac.Write(body)
		if r.Header.Get("Authorization") != "Bearer test-only-secret" || r.Header.Get("X-Overlay-Signature") != hex.EncodeToString(mac.Sum(nil)) {
			t.Error("missing machine authentication")
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"active": active.Load(), "grant": in})
	}))
	defer control.Close()
	cfg.Overlay = config.OverlayConfig{Enabled: true, BaseURL: control.URL, SharedSecret: "test-only-secret", Timeout: 5 * time.Second}
	svc.process(ctx, *g)
	var count int64
	db.Model(&model.Sample{}).Count(&count)
	if count != 0 {
		t.Fatal("paused grant published resources")
	}
	active.Store(true)
	svc.process(ctx, *g)
	if err := db.First(g, "id=?", g.ID).Error; err != nil || g.Status != "ready" {
		t.Fatalf("grant not ready: %+v %v", g, err)
	}
	var sample model.Sample
	if err := db.Where("uuid=? AND external_org_id=?", g.SampleUUID, g.OrgID).First(&sample).Error; err != nil {
		t.Fatal(err)
	}
	if sample.ExpiresAt == nil || !sample.ExpiresAt.Equal(approved.Add(720*time.Hour)) || sample.GetMatchedPair() == nil {
		t.Fatal("sample retention or links incorrect")
	}
	var task model.Task
	if err := db.Where("uuid=?", g.TaskUUID).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Executor != model.ExecutorExample || task.CVMInstanceID != "" || task.ResultImportStatus != model.ResultImportStatusSuccess {
		t.Fatal("example execution identity incorrect")
	}
	dataset, err := NewResultService(cfg).ensureParquetDataset(ctx, &task, task.TenantID, g.AttemptUUID, "snv-indel")
	if err != nil || dataset.ObjectKey != exampleObjectKey(g, m.Objects[4]) {
		t.Fatal("browser dataset could not resolve cloned Parquet", err)
	}
	catalog, err := NewResultDownloadService(cfg).Catalog(ctx, &task, false)
	if err != nil || len(catalog.BAMs) != 1 || catalog.BAMExpiresAt == nil || !catalog.BAMExpiresAt.Equal(approved.Add(168*time.Hour)) {
		t.Fatal("download catalog or BAM authorization deadline incorrect", err)
	}
	sources, _, err := NewResultPackageService(cfg).collectSources(ctx, store, resultPackagePrefix(&task))
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source.key, ".bam") || strings.HasSuffix(source.key, ".bai") {
			t.Fatal("result ZIP includes alignment data")
		}
	}
	if err := ActivateExampleTemplate(ctx, cfg, ""); err != nil {
		t.Fatal(err)
	}
	info, err := ExampleTemplate(ctx, cfg, "")
	if err != nil || info.Available {
		t.Fatal("disabled template still offered", err)
	}
	if _, err := svc.Enqueue(ctx, in); err != nil {
		t.Fatal("disabling offered version broke an existing grant", err)
	}
	if err := ActivateExampleTemplate(ctx, cfg, m.Version); err != nil {
		t.Fatal(err)
	}
	var assets []model.DataAsset
	if err := db.Where("external_org_id=?", g.OrgID).Find(&assets).Error; err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 {
		t.Fatal("missing FASTQ pair")
	}
	for _, a := range assets {
		if a.ExpiresAt == nil || !a.ExpiresAt.Equal(approved.Add(168*time.Hour)) {
			t.Fatal("FASTQ deadline reset")
		}
	}
	var job model.BAMRetentionJob
	if err := db.Where("task_uuid=?", g.TaskUUID).First(&job).Error; err != nil || !job.PurgeVersions || !job.ExpiresAt.Equal(approved.Add(168*time.Hour)) {
		t.Fatal("BAM retention registration incorrect", err)
	}
	for _, o := range m.Objects {
		r, err := store.open(ctx, exampleObjectKey(g, o))
		if err != nil {
			t.Fatal(err)
		}
		err = exampledata.VerifyObject(o, r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Add a second historical generation of BAM, then execute due cleanup.
	bam := m.Objects[2]
	f, _ := os.Open(filepath.Join(root, bam.Path))
	err = store.uploadExampleFile(ctx, exampleObjectKey(g, bam), f, bam.Size)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&job).Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewBAMRetentionService(cfg).Execute(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.open(ctx, exampleObjectKey(g, bam)); err == nil {
		t.Fatal("BAM still accessible")
	}
	if _, err := store.stat(ctx, exampleObjectKey(g, m.Objects[4])); err != nil {
		t.Fatal("BAM cleanup removed results")
	}
	if _, err := store.stat(ctx, exampleTemplatePrefix+m.Version+"/"+bam.Path); err != nil {
		t.Fatal("BAM cleanup touched template")
	}
	// A partially copied second organization expires at the original boundary.
	other := in
	other.ID = uuid.NewString()
	other.OrgID = uuid.NewString()
	g2, err := svc.Enqueue(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.copyObject(ctx, store, g2, m.Objects[0], exampleObjectKey(g2, m.Objects[0])); err != nil {
		t.Fatal(err)
	}
	svc.clock = func() time.Time { return approved.Add(168 * time.Hour) }
	status, code := svc.provision(ctx, g2)
	if status != "expired" || code != "" {
		t.Fatal("expiry cleanup failed", status, code)
	}
	if _, err := store.open(ctx, exampleObjectKey(g2, m.Objects[0])); err == nil {
		t.Fatal("partial copy retained")
	}
	if _, err := store.stat(ctx, exampleObjectKey(g, m.Objects[0])); err != nil {
		t.Fatal("cleanup crossed organization boundary")
	}
}
