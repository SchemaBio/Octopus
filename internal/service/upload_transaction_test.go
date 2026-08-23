package service

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newUploadTransactionTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func testUploadMetadata(fileCount int) (*model.UploadJob, []preparedUploadMetadata) {
	job := &model.UploadJob{UUID: "job-uuid", UserID: 1, ExternalOrgID: "org-1", Name: "upload", FileType: model.UploadFileTypeFastqPaired, Provider: model.UploadProviderS3, Status: model.UploadJobStatusPending}
	prepared := make([]preparedUploadMetadata, fileCount)
	for i := range prepared {
		fileID := "file-" + string(rune('1'+i))
		file := &model.UploadFile{UUID: fileID, JobUUID: job.UUID, FileName: fileID + ".fastq.gz", StorageKey: "organizations/org-1/" + fileID, FileSize: 100, ReadType: model.ReadTypeRead1, Status: model.FileStatusPending}
		asset := &model.DataAsset{UUID: fileID, ExternalOrgID: job.ExternalOrgID, CreatedBy: job.UserID, Provider: job.Provider, StorageKey: file.StorageKey, FileName: file.FileName, FileSize: file.FileSize, ReadType: file.ReadType, Status: model.FileStatusPending, Source: model.DataAssetSourceUpload}
		prepared[i] = preparedUploadMetadata{file: file, asset: asset}
	}
	return job, prepared
}

func expectQuotaCheck(mock sqlmock.Sqlmock, usedBytes int64) {
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtextextended($1, 0))")).
		WithArgs("octopus:storage-quota:org-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT COALESCE\(SUM\(file_size\), 0\) FROM "data_assets" WHERE external_org_id = \$1 AND status <> \$2`).
		WithArgs("org-1", model.FileStatusDeleted).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(usedBytes))
}

func expectCreate(mock sqlmock.Sqlmock, table string, id uint) {
	mock.ExpectQuery(`INSERT INTO "` + table + `"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))
}

func TestPersistUploadMetadataRollsBackWhenSecondFileFails(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	job, prepared := testUploadMetadata(2)
	mock.ExpectBegin()
	expectQuotaCheck(mock, 0)
	expectCreate(mock, "upload_jobs", 10)
	expectCreate(mock, "upload_files", 20)
	expectCreate(mock, "data_assets", 30)
	mock.ExpectQuery(`INSERT INTO "upload_files"`).WillReturnError(errors.New("file insert failed"))
	mock.ExpectRollback()

	if err := persistUploadMetadata(db, job, prepared, 200, 1000); err == nil {
		t.Fatal("expected second file failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestPersistUploadMetadataCommitsLinkedRecords(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	job, prepared := testUploadMetadata(1)
	mock.ExpectBegin()
	expectQuotaCheck(mock, 700)
	expectCreate(mock, "upload_jobs", 10)
	expectCreate(mock, "upload_files", 20)
	expectCreate(mock, "data_assets", 30)
	mock.ExpectCommit()

	if err := persistUploadMetadata(db, job, prepared, 100, 1000); err != nil {
		t.Fatalf("persistUploadMetadata: %v", err)
	}
	if job.ID != 10 || prepared[0].file.JobID != 10 || prepared[0].file.ID != 20 {
		t.Fatalf("records were not linked to generated IDs: job=%d file=%+v", job.ID, prepared[0].file)
	}
	if prepared[0].asset.UploadFileID == nil || *prepared[0].asset.UploadFileID != 20 {
		t.Fatalf("asset upload_file_id = %v, want 20", prepared[0].asset.UploadFileID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestPersistUploadMetadataRollsBackWhenAssetFails(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	job, prepared := testUploadMetadata(1)
	mock.ExpectBegin()
	expectQuotaCheck(mock, 0)
	expectCreate(mock, "upload_jobs", 10)
	expectCreate(mock, "upload_files", 20)
	mock.ExpectQuery(`INSERT INTO "data_assets"`).WillReturnError(errors.New("asset insert failed"))
	mock.ExpectRollback()

	if err := persistUploadMetadata(db, job, prepared, 100, 1000); err == nil {
		t.Fatal("expected asset failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestPersistUploadMetadataRejectsQuotaBeforeCreatingRecords(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	job, prepared := testUploadMetadata(1)
	mock.ExpectBegin()
	expectQuotaCheck(mock, 950)
	mock.ExpectRollback()

	err := persistUploadMetadata(db, job, prepared, 100, 1000)
	if err == nil || err.Error() != "organization storage quota exceeded" {
		t.Fatalf("error = %v, want quota exceeded", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestPersistUploadMetadataAppliesSharedQuotaToBED(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	job, prepared := testUploadMetadata(1)
	job.FileType = model.UploadFileTypeBed
	prepared[0].file.FileName = "panel.bed"
	prepared[0].file.ReadType = model.ReadTypeBed
	prepared[0].asset.FileName = "panel.bed"
	prepared[0].asset.ReadType = model.ReadTypeBed

	mock.ExpectBegin()
	expectQuotaCheck(mock, 950)
	mock.ExpectRollback()

	err := persistUploadMetadata(db, job, prepared, 100, 1000)
	if err == nil || err.Error() != "organization storage quota exceeded" {
		t.Fatalf("BED quota error = %v, want quota exceeded", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
