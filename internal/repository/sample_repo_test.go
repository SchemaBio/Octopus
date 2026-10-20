package repository

import (
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestSampleSearchKeepsTenantScopeAndSupportsCaseInsensitiveIdentifiers(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewSampleRepository()
	repo.Repository.db = db
	where := ` FROM "samples" WHERE external_org_id = \$1 AND \(internal_id ILIKE \$2 OR uuid ILIKE \$3\)`
	mock.ExpectQuery(`SELECT count\(\*\)`+where).WithArgs("org-a", "%ABCD%", "%ABCD%").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(105))
	mock.ExpectQuery(`SELECT \*`+where+` ORDER BY created_at DESC LIMIT \$4 OFFSET \$5`).WithArgs("org-a", "%ABCD%", "%ABCD%", 20, 100).WillReturnRows(sqlmock.NewRows([]string{"uuid"}).AddRow("abcd-sample"))
	rows, total, err := repo.PaginateByQuery(&model.SampleListQuery{OrgID: "org-a", Search: "ABCD", Page: 6, PageSize: 20})
	if err != nil || total != 105 || len(rows) != 1 {
		t.Fatalf("rows=%d total=%d err=%v", len(rows), total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
