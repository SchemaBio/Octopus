package repository

import (
	"errors"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestGroupedSNVHistoryReturnsCountError(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := &HistoryRepository{db: db}
	dbErr := errors.New("count failed")

	mock.ExpectQuery(`SELECT COUNT\(DISTINCT gene \|\| '-' \|\| hgv_sc \|\| '-' \|\| hgv_sp\) FROM "result_snv_indels" WHERE result_snv_indels\.reviewed = \$1`).
		WithArgs(true).
		WillReturnError(dbErr)

	rows, total, err := repo.GetGroupedSNVIndels(&model.HistoryListQuery{IncludeAll: true, Page: 1, PageSize: 20})
	if !errors.Is(err, dbErr) {
		t.Fatalf("GetGroupedSNVIndels error = %v, want count error", err)
	}
	if rows != nil || total != 0 {
		t.Fatalf("result = (%#v, %d), want nil, 0", rows, total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
