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

func TestFindCompletedDataAssetByStorageKeyUsesOrganizationScope(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewDataAssetRepository()
	repo.Repository.db = db
	mock.ExpectQuery(`SELECT \* FROM "data_assets" WHERE \(storage_key = \$1 AND status = \$2\) AND external_org_id = \$3 ORDER BY "data_assets"\."id" LIMIT \$4`).
		WithArgs("other-org/read1.fastq.gz", model.FileStatusCompleted, "org-a", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := repo.FindCompletedByStorageKey("other-org/read1.fastq.gz", model.OverlayActor{OrgID: "org-a", UserID: 42}); err == nil {
		t.Fatal("cross-organization file should not resolve")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAvailableDataAssetSearchPaginatesInsideOrganization(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewDataAssetRepository()
	repo.Repository.db = db
	where := ` FROM "data_assets" WHERE status <> \$1 AND external_org_id = \$2 AND \(file_name ILIKE \$3 OR uuid ILIKE \$4 OR internal_id ILIKE \$5\) AND \(status = \$6 AND \(expires_at IS NULL OR expires_at > \$7\)\) AND read_type = \$8`
	mock.ExpectQuery(`SELECT count\(\*\)`+where).
		WithArgs(model.FileStatusDeleted, "org-a", "%UUID%", "%UUID%", "%UUID%", model.FileStatusCompleted, sqlmock.AnyArg(), model.ReadType("vcf")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(105))
	mock.ExpectQuery(`SELECT \*`+where+` ORDER BY created_at DESC LIMIT \$9 OFFSET \$10`).
		WithArgs(model.FileStatusDeleted, "org-a", "%UUID%", "%UUID%", "%UUID%", model.FileStatusCompleted, sqlmock.AnyArg(), model.ReadType("vcf"), 20, 100).
		WillReturnRows(sqlmock.NewRows([]string{"uuid"}).AddRow("asset-105"))
	rows, total, err := repo.Paginate(&model.DataAssetListQuery{OrgID: "org-a", Search: " UUID ", AvailableOnly: true, ReadType: model.ReadType("vcf"), Page: 6, PageSize: 20})
	if err != nil || total != 105 || len(rows) != 1 {
		t.Fatalf("rows=%d total=%d err=%v", len(rows), total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
