package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestInterpretationCompletionTransitions(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name     string
		status   string
		phase    string
		at       *time.Time
		complete bool
		attempt  string
		update   bool
		reject   bool
	}{
		{name: "finish", status: "completed", phase: "terminal", complete: true, attempt: "attempt", update: true},
		{name: "finish pending interpretation", status: "pending_interpretation", complete: true, attempt: "attempt", update: true},
		{name: "reopen", status: "completed", at: &now, attempt: "attempt", update: true},
		{name: "idempotent finish", status: "completed", at: &now, complete: true, attempt: "attempt"},
		{name: "idempotent reopen", status: "completed", attempt: "attempt"},
		{name: "reject running", status: "running", complete: true, attempt: "attempt", reject: true},
		{name: "reject archiving", status: "completed", phase: "archiving", complete: true, attempt: "attempt", reject: true},
		{name: "reject stale finish", status: "completed", complete: true, attempt: "old", reject: true},
		{name: "reject stale reopen", status: "completed", at: &now, attempt: "old", reject: true},
	}
	for _, tt := range tests {
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
			authorized := &model.Task{UUID: "task", TenantID: "tenant"}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*tasks.*FOR UPDATE`).WithArgs("task", "tenant", 1).WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "tenant_id", "status", "execution_phase", "execution_attempt_id", "interpretation_completed_at"}).AddRow("id", "task", "tenant", tt.status, tt.phase, "attempt", tt.at))
			if tt.reject {
				mock.ExpectRollback()
			} else {
				if tt.update {
					var at interface{} = nil
					actor := ""
					if tt.complete {
						at = sqlmock.AnyArg()
						actor = "doctor"
					}
					mock.ExpectExec(`UPDATE "tasks" SET "interpretation_completed_at"=\$1,"interpretation_completed_by"=\$2,"updated_at"=\$3 WHERE`).WithArgs(at, actor, sqlmock.AnyArg(), "id").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				mock.ExpectCommit()
			}
			got, err := (&TaskService{}).SetInterpretationCompleted(context.Background(), authorized, tt.complete, tt.attempt, "doctor")
			if tt.reject {
				if err == nil {
					t.Fatal("accepted invalid transition")
				}
				if tt.attempt == "old" && !errors.Is(err, ErrInterpretationAttemptChanged) {
					t.Fatalf("wrong stale attempt error: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if (got.InterpretationCompletedAt != nil) != tt.complete || string(got.Status) != tt.status || got.ExecutionAttemptID != "attempt" {
					t.Fatalf("unexpected transition: %+v", got)
				}
				if tt.update && tt.complete && got.InterpretationCompletedBy != "doctor" {
					t.Fatal("missing actor")
				}
				if tt.update && !tt.complete && got.InterpretationCompletedBy != "" {
					t.Fatal("actor not cleared")
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
