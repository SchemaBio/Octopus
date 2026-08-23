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
