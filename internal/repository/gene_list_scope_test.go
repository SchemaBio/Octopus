package repository

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestGeneListRepositoryScopesLookupToOwner(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewGeneListRepository()
	repo.Repository.db = db

	query := `SELECT * FROM "gene_lists" WHERE id = $1 AND created_by = $2 ORDER BY "gene_lists"."id" LIMIT $3`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("list-1", uint(42), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_by", "genes_json"}).
			AddRow("list-1", 42, `["BRCA1"]`))

	geneList, err := repo.FindScopedByStringID("list-1", model.OverlayActor{UserID: 42})
	if err != nil {
		t.Fatalf("FindScopedByStringID: %v", err)
	}
	if geneList.CreatedBy != 42 || len(geneList.GetGenes()) != 1 || geneList.GetGenes()[0] != "BRCA1" {
		t.Fatalf("unexpected gene list: %#v", geneList)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestGeneListRepositorySuperAdminLookupIsUnscoped(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewGeneListRepository()
	repo.Repository.db = db

	query := `SELECT * FROM "gene_lists" WHERE id = $1 ORDER BY "gene_lists"."id" LIMIT $2`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("list-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_by"}).AddRow("list-1", 99))

	geneList, err := repo.FindScopedByStringID("list-1", model.OverlayActor{Role: string(model.SystemRoleSuperAdmin)})
	if err != nil {
		t.Fatalf("FindScopedByStringID: %v", err)
	}
	if geneList.CreatedBy != 99 {
		t.Fatalf("CreatedBy = %d, want 99", geneList.CreatedBy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestResultRepositoryUsesResolvedGeneListGenes(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := &ResultRepository{db: db}

	mock.ExpectQuery(`SELECT count\(\*\) FROM "result_snv_indels" WHERE task_id = \$1 AND gene IN \(\$2,\$3\)`).
		WithArgs("task-1", "BRCA1", "BRCA2").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT \* FROM "result_snv_indels" WHERE task_id = \$1 AND gene IN \(\$2,\$3\) ORDER BY acmg_classification ASC, gene ASC LIMIT \$4`).
		WithArgs("task-1", "BRCA1", "BRCA2", 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "gene"}).AddRow("variant-1", "task-1", "BRCA1"))

	rows, total, err := repo.PaginateSNVIndels(&model.SNVIndelListQuery{
		TaskID:        "task-1",
		GeneListID:    "list-1",
		GeneListGenes: []string{"BRCA1", "BRCA2"},
		Page:          1,
		PageSize:      20,
	})
	if err != nil {
		t.Fatalf("PaginateSNVIndels: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Gene != "BRCA1" {
		t.Fatalf("unexpected result: total=%d rows=%#v", total, rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestResultRepositoryEmptyResolvedGeneListMatchesNothing(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := &ResultRepository{db: db}

	mock.ExpectQuery(`SELECT count\(\*\) FROM "result_snv_indels" WHERE task_id = \$1 AND 1 = 0`).
		WithArgs("task-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "result_snv_indels" WHERE task_id = \$1 AND 1 = 0 ORDER BY acmg_classification ASC, gene ASC LIMIT \$2`).
		WithArgs("task-1", 20).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	rows, total, err := repo.PaginateSNVIndels(&model.SNVIndelListQuery{
		TaskID:     "task-1",
		GeneListID: "empty-list",
		Page:       1,
		PageSize:   20,
	})
	if err != nil {
		t.Fatalf("PaginateSNVIndels: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("empty gene list returned total=%d rows=%#v", total, rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
