package repository

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

func TestTaskRepositoryStandaloneListExcludesOrganizationTasks(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewTaskRepository()
	repo.Repository.db = db

	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE \(tasks\.external_org_id = '' AND tasks\.created_by = \$1\) AND "tasks"\."deleted_at" IS NULL`).
		WithArgs(uint(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE \(tasks\.external_org_id = '' AND tasks\.created_by = \$1\) AND "tasks"\."deleted_at" IS NULL ORDER BY created_at DESC LIMIT \$2`).
		WithArgs(uint(42), 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, _, err := repo.PaginateByQuery(&model.TaskListQuery{CreatedBy: 42}); err != nil {
		t.Fatalf("PaginateByQuery: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestTaskRepositoryPlatformAdminCanFilterByOrganization(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewTaskRepository()
	repo.Repository.db = db

	mock.ExpectQuery(`SELECT count\(\*\) FROM "tasks" WHERE external_org_id = \$1 AND "tasks"\."deleted_at" IS NULL`).
		WithArgs("org-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT \* FROM "tasks" WHERE external_org_id = \$1 AND "tasks"\."deleted_at" IS NULL ORDER BY created_at DESC LIMIT \$2`).
		WithArgs("org-1", 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("task-row-1"))

	rows, total, err := repo.PaginateByQuery(&model.TaskListQuery{IncludeAll: true, OrgID: "org-1"})
	if err != nil {
		t.Fatalf("PaginateByQuery: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("PaginateByQuery = (%d rows, total %d), want (1, 1)", len(rows), total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestTaskRepositoryDeleteByIDUsesSoftDelete(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewTaskRepository()
	repo.Repository.db = db

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET "deleted_at"=\$1 WHERE id = \$2 AND "tasks"\."deleted_at" IS NULL`).
		WithArgs(sqlmock.AnyArg(), "task-row-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.DeleteByID("task-row-1"); err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestResultImportBatchStandaloneListExcludesOrganizationTasks(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewResultImportBatchRepository()
	repo.Repository.db = db

	mock.ExpectQuery(`SELECT count\(\*\) FROM "result_import_batches" LEFT JOIN tasks ON tasks\.uuid = result_import_batches\.task_uuid WHERE tasks\.external_org_id = '' AND tasks\.created_by = \$1`).
		WithArgs(uint(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`(?s)SELECT .* FROM "result_import_batches" LEFT JOIN tasks ON tasks\.uuid = result_import_batches\.task_uuid WHERE tasks\.external_org_id = '' AND tasks\.created_by = \$1 ORDER BY result_import_batches\.started_at DESC LIMIT \$2`).
		WithArgs(uint(42), 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, _, err := repo.PaginateByQuery(&model.ResultImportBatchListQuery{UserID: 42}); err != nil {
		t.Fatalf("PaginateByQuery: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestHistoryRepositoryStandaloneScopeExcludesOrganizationTasks(t *testing.T) {
	db, _ := newUploadRepositoryTestDB(t)
	repo := &HistoryRepository{db: db}
	query := &model.HistoryListQuery{CreatedBy: 42}

	var rows []model.SNVIndel
	statement := repo.scopedHistory(&model.SNVIndel{}, "result_snv_indels", query).
		Session(&gorm.Session{DryRun: true}).
		Find(&rows).
		Statement
	sql := statement.SQL.String()
	for _, expected := range []string{
		"JOIN tasks ON tasks.uuid = result_snv_indels.task_id",
		"tasks.external_org_id = '' AND tasks.created_by =",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("history SQL %q does not contain %q", sql, expected)
		}
	}
}
