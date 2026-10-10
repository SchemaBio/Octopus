package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDownloadIssueRetainsPaidWindowAcrossIPChanges(t *testing.T) {
	for _, kind := range []string{"bam", "zip"} {
		t.Run(kind, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			previous := database.DB
			database.DB = db
			defer func() { database.DB = previous }()
			paidUntil := time.Now().UTC().Add(2 * time.Hour)
			charged := time.Now().UTC().Add(-time.Hour)
			task := &model.Task{UUID: "task", ExecutionAttemptID: "attempt", ExternalOrgID: "org"}
			actor := model.OverlayActor{UserID: 7, OrgID: "org"}
			rows := func() *sqlmock.Rows {
				return sqlmock.NewRows([]string{"id", "task_uuid", "attempt_id", "org_id", "user_id", "client_ip", "kind", "object_key", "filename", "size_bytes", "e_tag", "credits", "link_expires_at", "charged_at", "issue_count"}).AddRow("grant", "task", "attempt", "org", 7, "198.51.100.10", kind, "file", "file", 100, `"etag"`, 1, paidUntil, charged, 0)
			}
			query := regexp.QuoteMeta(`SELECT * FROM "result_downloads" WHERE id = $1 AND task_uuid = $2 AND attempt_id = $3 AND user_id = $4 AND org_id = $5 ORDER BY "result_downloads"."id" LIMIT $6 FOR UPDATE`)
			mock.ExpectBegin()
			mock.ExpectQuery(query).WithArgs("grant", "task", "attempt", uint(7), "org", 1).WillReturnRows(rows())
			mock.ExpectCommit()
			mock.ExpectBegin()
			mock.ExpectQuery(query).WithArgs("grant", "task", "attempt", uint(7), "org", 1).WillReturnRows(rows())
			mock.ExpectExec(`UPDATE "result_downloads"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			storageCalls := 0
			storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				storageCalls++
				if r.Method != "HEAD" {
					t.Error("unexpected file transfer through application")
				}
				w.Header().Set("Content-Length", "100")
				w.Header().Set("ETag", `"etag"`)
			}))
			defer storage.Close()
			charges := 0
			billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { charges++; w.WriteHeader(500) }))
			defer billing.Close()
			cfg := &config.Config{Storage: config.StorageConfig{S3Endpoint: storage.URL, S3Bucket: "bucket-1234567890", S3Region: "ap-guangzhou", S3UsePathStyle: true, S3AccessKey: "test", S3SecretKey: "test"}}
			svc := &ResultDownloadService{cfg: cfg, overlay: NewOverlayClient(config.OverlayConfig{Enabled: true, BaseURL: billing.URL}), signDownload: func(ctx context.Context, c config.StorageConfig, key, filename string, expires time.Time) (string, error) {
				if expires.After(paidUntil) || time.Until(expires) > 30*time.Minute {
					t.Fatal("extended authorization")
				}
				return "https://bucket.cos.ap-guangzhou.myqcloud.com/file", nil
			}}
			result, err := svc.Issue(context.Background(), task, actor, "203.0.113.20", "grant")
			if err != nil {
				t.Fatal(err)
			}
			if charges != 0 || storageCalls != 1 || result["ip_bound"] != false || result["remaining_issues"] != 11 || *result["grant_expires_at"].(*time.Time) != paidUntil {
				b, _ := json.Marshal(result)
				t.Fatalf("unexpected refresh: charges=%d storage=%d %s", charges, storageCalls, b)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
