package repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestFindCompletedDataAssetByStorageKeyUsesStandaloneScope(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewDataAssetRepository()
	repo.Repository.db = db

	mock.ExpectQuery(`SELECT \* FROM "data_assets" WHERE \(storage_key = \$1 AND status = \$2\) AND \(external_org_id = '' AND created_by = \$3\) ORDER BY "data_assets"\."id" LIMIT \$4`).
		WithArgs("private/read1.fastq.gz", model.FileStatusCompleted, uint(42), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	if _, err := repo.FindCompletedByStorageKey("private/read1.fastq.gz", model.OverlayActor{UserID: 42}); err != nil {
		t.Fatalf("FindCompletedByStorageKey: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
