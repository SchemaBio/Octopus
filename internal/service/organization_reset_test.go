package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestValidateResetIdentity(t *testing.T) {
	if err := ValidateResetIdentity(uuid.NewString(), uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	for _, org := range []string{"", "../other", "org:123", strings.ToUpper(uuid.NewString())} {
		if ValidateResetIdentity(org, uuid.NewString()) == nil {
			t.Fatal("unsafe namespace accepted")
		}
	}
}

func organizationResetDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ORG_RESET_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated ORG_RESET_TEST_DSN")
	}
	if !strings.Contains(dsn, "host=127.0.0.1 ") || !strings.Contains(dsn, "dbname=org_reset_test_local ") {
		t.Fatal("refusing non-test database")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := "octopus_reset_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Error)})
	if err != nil {
		t.Fatal(err)
	}
	old := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = old
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
		_ = root.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		rootDB, _ := root.DB()
		_ = rootDB.Close()
	})
	if err := database.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := MigrateExampleData(&config.Config{Edition: "saas"}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnableSaaSRetention(); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestOrganizationResetPostgresIsolationCleanupAndReplay(t *testing.T) {
	db := organizationResetDB(t)
	ctx := context.Background()
	org, other, id := uuid.NewString(), uuid.NewString(), uuid.NewString()
	storageCalls := 0
	failStorage := true
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storageCalls++
		if r.URL.Query().Get("prefix") != "organizations/"+org+"/" && !strings.HasPrefix(r.URL.Query().Get("prefix"), "organizations/"+org+"/workflows/") {
			t.Error("cross-tenant storage cleanup", r.URL)
		}
		if failStorage {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.URL.Query().Has("versions"):
			_, _ = w.Write([]byte(`<ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></ListVersionsResult>`))
		case r.URL.Query().Has("uploads"):
			_, _ = w.Write([]byte(`<ListMultipartUploadsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></ListMultipartUploadsResult>`))
		default:
			_, _ = w.Write([]byte(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></ListBucketResult>`))
		}
	}))
	defer storage.Close()
	overlay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["org_id"] != org {
			t.Error("cross-org overlay")
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": true})
	}))
	defer overlay.Close()
	cfg := &config.Config{Edition: "saas", Overlay: config.OverlayConfig{Enabled: true, BaseURL: overlay.URL, SharedSecret: "test", Timeout: time.Second}, Storage: config.StorageConfig{Provider: "s3", S3Endpoint: storage.URL, S3Bucket: "test", S3Region: "us-east-1", S3AccessKey: "test", S3SecretKey: "test", S3UsePathStyle: true}}
	cfg.Task.OutputDir = t.TempDir()
	cfg.Task.ArchiveDir = t.TempDir()
	cfg.ResultQuery.CacheDir = t.TempDir()
	cfg.ResultQuery.ReferenceDir = t.TempDir()
	for _, tenant := range []string{org, other} {
		if err := db.Exec("INSERT INTO samples(uuid,external_org_id,internal_id,created_by,created_at)VALUES(?,?,?,1,now())", uuid.NewString(), tenant, tenant).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.Project{ExternalOrgID: tenant, ProjectCode: tenant, ProjectName: "Project", CreatedBy: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	task := uuid.NewString()
	if err := db.Exec("INSERT INTO tasks(id,uuid,external_org_id,tenant_id,status,executor,created_by,created_at)VALUES(?,?,?,?,?,?,1,now())", task, task, org, "org:"+org, "running", "example").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := StepOrganizationReset(ctx, cfg, org, id, "prepare"); err == nil {
		t.Fatal("active work accepted")
	}
	if err := db.Model(&model.Task{}).Where("uuid=?", task).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if state, err := StepOrganizationReset(ctx, cfg, org, id, "prepare"); err != nil || state != "clearing" {
		t.Fatal(state, err)
	}
	if err := db.Create(&model.Project{ExternalOrgID: org, ProjectCode: uuid.NewString(), ProjectName: "late write"}).Error; err == nil {
		t.Fatal("write bypassed reset fence")
	}
	if _, err := StepOrganizationReset(ctx, cfg, org, id, "step"); err == nil {
		t.Fatal("storage failure silently accepted")
	}
	var tasks int64
	db.Unscoped().Model(&model.Task{}).Where("external_org_id=?", org).Count(&tasks)
	if tasks != 1 {
		t.Fatal("identity removed before storage cleanup")
	}
	failStorage = false
	for i := 0; i < 3; i++ {
		state, err := StepOrganizationReset(ctx, cfg, org, id, "step")
		if err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			break
		}
	}
	for _, table := range []string{"tasks", "samples", "projects"} {
		var n int64
		if err := db.Unscoped().Table(table).Where("external_org_id=?", org).Count(&n).Error; err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	var n int64
	db.Unscoped().Model(&model.Sample{}).Where("external_org_id=?", other).Count(&n)
	if n != 1 {
		t.Fatal("other tenant erased")
	}
	newProject := model.Project{ExternalOrgID: org, ProjectCode: uuid.NewString(), ProjectName: "new workspace"}
	if err := db.Create(&newProject).Error; err != nil {
		t.Fatal(err)
	}
	calls := storageCalls
	if state, err := StepOrganizationReset(ctx, cfg, org, id, "prepare"); err != nil || state != "completed" {
		t.Fatal("replay", state, err)
	}
	if storageCalls != calls {
		t.Fatal("replay repeated object cleanup")
	}
	db.Model(&model.Project{}).Where("id=?", newProject.ID).Count(&n)
	if n != 1 {
		t.Fatal("replay erased new workspace")
	}
	if _, err := StepOrganizationReset(ctx, cfg, other, id, "step"); err == nil {
		t.Fatal("receipt replayed for another tenant")
	}
}
