package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCVMRetryRecordsTimeOncePerAcceptedAttempt(t *testing.T) {
	for _, tt := range []struct {
		name, status, phase string
		retry               bool
		duplicate           bool
		reject              bool
	}{
		{name: "failed retry", status: "failed", phase: "terminal", retry: true},
		{name: "cancelled retry", status: "cancelled", phase: "terminal", retry: true},
		{name: "initial execution", status: "queued"},
		{name: "duplicate retry request", status: "queued", phase: "running", duplicate: true},
		{name: "rejected request", status: "completed", reject: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			prior := database.DB
			database.DB = db
			defer func() { database.DB = prior }()
			original := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			var previousRetry interface{}
			if tt.duplicate {
				previousRetry = original.Add(time.Hour)
			}
			before := time.Now().UTC()
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*tasks.*FOR UPDATE`).WithArgs("task", 1).WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "executor", "status", "execution_phase", "execution_attempt_id", "created_at", "retry_started_at"}).AddRow("id", "task", model.ExecutorCVM, tt.status, tt.phase, "attempt", original, previousRetry))
			if tt.reject {
				mock.ExpectRollback()
			} else {
				if !tt.duplicate {
					mock.ExpectQuery(`SELECT .*task_data_assets`).WithArgs("task").WillReturnRows(sqlmock.NewRows([]string{"id"}))
					mock.ExpectExec(`UPDATE "tasks" SET .*"retry_started_at"=`).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec(`INSERT INTO "c_vm_submissions"`).WillReturnResult(sqlmock.NewResult(0, 1))
				}
				mock.ExpectCommit()
			}
			got, err := (&TaskService{}).enqueueCVM(context.Background(), "task", model.OverlayActor{})
			if tt.reject {
				if err == nil {
					t.Fatal("invalid retry accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !got.CreatedAt.Equal(original) {
					t.Fatal("original creation time overwritten")
				}
				if tt.retry && (got.RetryStartedAt == nil || got.RetryStartedAt.Before(before) || got.RetryStartedAt.After(time.Now().UTC())) {
					t.Fatalf("missing accepted retry time: %v", got.RetryStartedAt)
				}
				if tt.duplicate && (got.RetryStartedAt == nil || !got.RetryStartedAt.Equal(previousRetry.(time.Time))) {
					t.Fatal("duplicate request changed retry time")
				}
				if !tt.retry && !tt.duplicate && got.RetryStartedAt != nil {
					t.Fatal("initial execution treated as retry")
				}
				if got.ToResponse().RetryStartedAt != got.RetryStartedAt || got.ToDetailResponse().RetryStartedAt != got.RetryStartedAt {
					t.Fatal("retry time missing from response")
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
