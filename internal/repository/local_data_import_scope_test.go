package repository

import (
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestSampleMatchingUsesSharedAssetsOnlyInSelfHostedMode(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewDataAssetRepository()
	repo.db = db
	mock.ExpectQuery(`SELECT \* FROM "data_assets" WHERE status = \$1 AND COALESCE\(external_org_id, ''\) = '' ORDER BY created_at DESC`).WithArgs(model.FileStatusCompleted).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	if _, err := repo.FindCompletedByScope("", 42, true); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`SELECT \* FROM "data_assets" WHERE status = \$1 AND \(external_org_id = '' AND created_by = \$2\) ORDER BY created_at DESC`).WithArgs(model.FileStatusCompleted, uint(42)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := repo.FindCompletedByScope("", 42, false); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`SELECT \* FROM "data_assets" WHERE status = \$1 AND external_org_id = \$2 ORDER BY created_at DESC`).WithArgs(model.FileStatusCompleted, "org-a").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := repo.FindCompletedByScope("org-a", 42, false); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
