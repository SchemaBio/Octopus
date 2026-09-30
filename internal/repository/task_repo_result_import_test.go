package repository

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestFinishResultImportOnlyUpdatesCurrentImportAttempt(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewTaskRepository()
	repo.Repository.db = db

	finishedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	query := `UPDATE "tasks" SET "result_import_error"=$1,"result_import_fingerprint"=$2,"result_import_started_at"=$3,"result_import_status"=$4,"result_imported_at"=$5,"updated_at"=$6 WHERE (uuid = $7 AND execution_attempt_id = $8 AND result_import_status = $9 AND result_import_attempts = $10) AND "tasks"."deleted_at" IS NULL`
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("invalid archive row", "", nil, model.ResultImportStatusFailed, nil, finishedAt,
			"task-uuid", "attempt-uuid", model.ResultImportStatusRunning, 3).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	task := &model.Task{UUID: "task-uuid", ExecutionAttemptID: "attempt-uuid"}
	if err := repo.FinishResultImport(task, 3, model.ResultImportStatusFailed, "invalid archive row", "", finishedAt); err != nil {
		t.Fatalf("FinishResultImport: %v", err)
	}
	if task.ResultImportStatus != model.ResultImportStatusFailed || task.ResultImportedAt != nil {
		t.Fatalf("failed import updated task metadata unexpectedly: status=%q importedAt=%v", task.ResultImportStatus, task.ResultImportedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestFinishResultImportRejectsStaleAttempt(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewTaskRepository()
	repo.Repository.db = db

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "tasks" SET .* WHERE \(uuid = \$7 AND execution_attempt_id = \$8 AND result_import_status = \$9 AND result_import_attempts = \$10\) AND "tasks"\."deleted_at" IS NULL`).
		WithArgs("", "fingerprint", nil, model.ResultImportStatusSuccess, sqlmock.AnyArg(), sqlmock.AnyArg(),
			"task-uuid", "old-attempt", model.ResultImportStatusRunning, 1).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	task := &model.Task{UUID: "task-uuid", ExecutionAttemptID: "old-attempt"}
	err := repo.FinishResultImport(task, 1, model.ResultImportStatusSuccess, "", "fingerprint", time.Now())
	if err == nil {
		t.Fatal("FinishResultImport succeeded for a stale attempt")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestBeginResultImportForAttemptRejectsUnselectedAttemptWithoutWriting(t *testing.T) {
	db, mock := newUploadRepositoryTestDB(t)
	repo := NewTaskRepository()
	repo.Repository.db = db

	task := &model.Task{UUID: "task-uuid", ExecutionAttemptID: "current-attempt"}
	if _, err := repo.BeginResultImportForAttempt(task, "old-attempt", "/archive", "fingerprint", time.Now(), 15*time.Minute); err == nil {
		t.Fatal("result import accepted an attempt other than the task's current attempt")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database writes: %v", err)
	}
}
