package handler

import (
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTenantStorageUsesAssetsWithoutUploadHistory(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	old := database.DB
	database.DB = db
	defer func() { database.DB = old }()
	mock.ExpectQuery(`FROM "tasks"`).WillReturnRows(sqlmock.NewRows([]string{"org_id", "task_count"}))
	mock.ExpectQuery(`FROM "tasks"`).WillReturnRows(sqlmock.NewRows([]string{"org_id", "result_import_failure_count"}))
	mock.ExpectQuery(`FROM "data_assets" WHERE external_org_id IN .* AND status =`).WithArgs("org-a", "org-b", model.FileStatusCompleted).
		WillReturnRows(sqlmock.NewRows([]string{"org_id", "uploaded_file_count", "uploaded_bytes"}).AddRow("org-a", 2, int64(2744095409)))
	mock.ExpectQuery(`FROM "upload_files"`).WillReturnRows(sqlmock.NewRows([]string{"org_id", "stale_upload_count"}))
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest("GET", "/api/v1/admin/tenant-stats?org_id=org-a&org_id=org-b", nil)
	NewAdminTenantStatsHandler().GetTenantStats(ctx)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var response struct {
		Data []model.AdminTenantStats `json:"data"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 2 || response.Data[0].UploadedBytes != 2744095409 || response.Data[0].UploadedFileCount != 2 || response.Data[1].UploadedBytes != 0 {
		t.Fatalf("unexpected aggregate: %+v", response.Data)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestParseAdminTenantStatsOrgIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("GET", "/api/v1/admin/tenant-stats?org_id=org-a&org_id=org-b,org-a", nil)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request

	ids, err := parseAdminTenantStatsOrgIDs(ctx)
	if err != nil {
		t.Fatalf("parseAdminTenantStatsOrgIDs returned error: %v", err)
	}
	if len(ids) != 2 || ids[0] != "org-a" || ids[1] != "org-b" {
		t.Fatalf("unexpected normalized tenant IDs: %#v", ids)
	}
}

func TestParseAdminTenantStatsOrgIDsCapsRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	query := ""
	for i := 0; i < maxAdminTenantStatsOrgIDs+1; i++ {
		if i > 0 {
			query += "&"
		}
		query += "org_id=org-" + string(rune('a'+(i%26))) + "-" + string(rune('0'+(i/26)))
	}
	request := httptest.NewRequest("GET", "/api/v1/admin/tenant-stats?"+query, nil)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request
	if _, err := parseAdminTenantStatsOrgIDs(ctx); err == nil {
		t.Fatal("expected more-than-100 tenant IDs to be rejected")
	}
}
