package repository

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newUploadRepositoryTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db, mock
}

func TestCompletedStorageStatsIncludesBEDAndScannerAssets(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewUploadFileRepository()
	repo.Repository.db = db

	// The aggregate represents one completed FASTQ/scanner asset (2 KiB) plus
	// one completed BED (1 KiB). Keep the SQL free of upload_files joins and
	// read_type filters: both consume the organization's shared capacity.
	query := `SELECT COUNT(*) AS total, COALESCE(SUM(data_assets.file_size), 0) AS total_bytes FROM "data_assets" WHERE data_assets.external_org_id = $1 AND data_assets.status = $2`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("org-1", model.FileStatusCompleted).
		WillReturnRows(sqlmock.NewRows([]string{"total", "total_bytes"}).AddRow(2, 3*1024))

	total, bytes, err := repo.CompletedStorageStats(&model.UploadFileListQuery{ExternalOrgID: "org-1"})
	if err != nil {
		t.Fatalf("CompletedStorageStats: %v", err)
	}
	if total != 2 || bytes != 3*1024 {
		t.Fatalf("CompletedStorageStats = (%d, %d), want (2, %d)", total, bytes, 3*1024)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestStorageCapacityStatsSeparatesReservationsInTenantSnapshot(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewUploadFileRepository()
	repo.Repository.db = db
	query := `SELECT COUNT(CASE WHEN data_assets.status = $1 THEN 1 END) AS total, COALESCE(SUM(CASE WHEN data_assets.status = $2 THEN data_assets.file_size ELSE 0 END), 0) AS total_bytes, COALESCE(SUM(CASE WHEN data_assets.status <> $3 THEN data_assets.file_size ELSE 0 END), 0) AS reserved_bytes FROM "data_assets" WHERE data_assets.external_org_id = $4 AND data_assets.status <> $5`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(model.FileStatusCompleted, model.FileStatusCompleted, model.FileStatusCompleted, "org-1", model.FileStatusDeleted).
		WillReturnRows(sqlmock.NewRows([]string{"total", "total_bytes", "reserved_bytes"}).AddRow(2, 20, 9))
	stats, err := repo.StorageCapacityStats(&model.UploadFileListQuery{ExternalOrgID: "org-1"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalBytes != 20 || stats.ReservedBytes != 9 || stats.Total != 2 {
		t.Fatalf("unexpected capacity snapshot: %+v", stats)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUploadJobListStandaloneScopeExcludesOrganizationJobs(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewUploadJobRepository()
	repo.Repository.db = db

	mock.ExpectQuery(`SELECT count\(\*\) FROM "upload_jobs" WHERE status <> \$1 AND \(external_org_id = '' AND user_id = \$2\)`).
		WithArgs(model.UploadJobStatusDeleted, uint(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "upload_jobs" WHERE status <> \$1 AND \(external_org_id = '' AND user_id = \$2\) ORDER BY created_at DESC LIMIT \$3`).
		WithArgs(model.UploadJobStatusDeleted, uint(42), 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	actor := model.OverlayActor{UserID: 42}
	if _, _, err := repo.PaginateByQuery(&model.UploadJobListQuery{}, actor); err != nil {
		t.Fatalf("PaginateByQuery: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestUploadFileListStandaloneScopeExcludesOrganizationFiles(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewUploadFileRepository()
	repo.Repository.db = db

	join := `FROM "upload_files" JOIN upload_jobs ON upload_jobs.id = upload_files.job_id LEFT JOIN data_assets ON data_assets.upload_file_id = upload_files.id`
	scope := `WHERE upload_jobs.external_org_id = '' AND upload_jobs.user_id = $1`
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) ` + join + ` ` + scope)).
		WithArgs(uint(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`(?s)SELECT .* `+regexp.QuoteMeta(join+` `+scope)+` ORDER BY upload_files\.created_at DESC LIMIT \$2`).
		WithArgs(uint(42), 10).
		WillReturnRows(sqlmock.NewRows([]string{"uuid"}))

	if _, _, err := repo.PaginateFilesByQuery(&model.UploadFileListQuery{UserID: 42}); err != nil {
		t.Fatalf("PaginateFilesByQuery: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCompletedStorageStatsSharedInstanceUsesAssetScope(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewUploadFileRepository()
	repo.Repository.db = db
	// Completed storage is read directly from data_assets, including scanner
	// assets with no upload job. Shared scope must use that table as well.
	query := `SELECT COUNT(*) AS total, COALESCE(SUM(data_assets.file_size), 0) AS total_bytes FROM "data_assets" WHERE COALESCE(data_assets.external_org_id, '') = '' AND data_assets.status = $1`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(model.FileStatusCompleted).
		WillReturnRows(sqlmock.NewRows([]string{"total", "total_bytes"}).AddRow(3, 4096))
	total, size, err := repo.CompletedStorageStats(&model.UploadFileListQuery{SharedInstance: true, IncludeAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || size != 4096 {
		t.Fatalf("unexpected shared storage statistics: %d %d", total, size)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
