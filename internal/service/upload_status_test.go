package service

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestDesiredUploadJobStatus(t *testing.T) {
	tests := []struct {
		name    string
		current model.UploadJobStatus
		counts  uploadJobStatusCounts
		want    model.UploadJobStatus
	}{
		{name: "active file keeps job uploading", current: model.UploadJobStatusUploading, counts: uploadJobStatusCounts{Total: 2, Active: 1, Completed: 1}, want: model.UploadJobStatusUploading},
		{name: "all files completed", current: model.UploadJobStatusUploading, counts: uploadJobStatusCounts{Total: 2, Completed: 2}, want: model.UploadJobStatusCompleted},
		{name: "all files deleted", current: model.UploadJobStatusUploading, counts: uploadJobStatusCounts{Total: 2, Deleted: 2}, want: model.UploadJobStatusDeleted},
		{name: "completed and deleted is failed", current: model.UploadJobStatusUploading, counts: uploadJobStatusCounts{Total: 2, Completed: 1, Deleted: 1}, want: model.UploadJobStatusFailed},
		{name: "failed and completed is failed", current: model.UploadJobStatusUploading, counts: uploadJobStatusCounts{Total: 2, Completed: 1, Failed: 1}, want: model.UploadJobStatusFailed},
		{name: "terminal deletion cannot be revived", current: model.UploadJobStatusDeleting, counts: uploadJobStatusCounts{Total: 2, Completed: 2}, want: model.UploadJobStatusDeleting},
		{name: "deleted job cannot be revived", current: model.UploadJobStatusDeleted, counts: uploadJobStatusCounts{Total: 1, Completed: 1}, want: model.UploadJobStatusDeleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := desiredUploadJobStatus(tt.current, tt.counts); got != tt.want {
				t.Fatalf("desiredUploadJobStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReconcileUploadJobStatusPersistsMixedDeletionAsFailed(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "upload_jobs" WHERE "upload_jobs"."id" = $1 ORDER BY "upload_jobs"."id" LIMIT $2 FOR UPDATE`)).
		WithArgs(uint(7), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(7, model.UploadJobStatusUploading))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) AS count FROM "upload_files" WHERE job_id = $1 GROUP BY "status"`)).
		WithArgs(uint(7)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "count"}).
			AddRow(model.FileStatusCompleted, 1).
			AddRow(model.FileStatusDeleted, 1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "upload_jobs" SET "status"=$1,"updated_at"=$2 WHERE "id" = $3`)).
		WithArgs(model.UploadJobStatusFailed, sqlmock.AnyArg(), uint(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	status, err := reconcileUploadJobStatus(db, 7)
	if err != nil {
		t.Fatalf("reconcileUploadJobStatus: %v", err)
	}
	if status != model.UploadJobStatusFailed {
		t.Fatalf("status = %q, want failed", status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestReconcileUploadJobStatusDoesNotReviveDeletingJob(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "upload_jobs" WHERE "upload_jobs"."id" = $1 ORDER BY "upload_jobs"."id" LIMIT $2 FOR UPDATE`)).
		WithArgs(uint(8), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(8, model.UploadJobStatusDeleting))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) AS count FROM "upload_files" WHERE job_id = $1 GROUP BY "status"`)).
		WithArgs(uint(8)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "count"}).AddRow(model.FileStatusCompleted, 2))
	status, err := reconcileUploadJobStatus(db, 8)
	if err != nil {
		t.Fatalf("reconcileUploadJobStatus: %v", err)
	}
	if status != model.UploadJobStatusDeleting {
		t.Fatalf("status = %q, want deleting", status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
