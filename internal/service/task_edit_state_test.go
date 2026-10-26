package service

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestTaskEditingChecksCurrentLockedState(t *testing.T) {
	for _, state := range []string{"queued", "failed", "waiting_for_data", "running", "completed", "pending_interpretation", "cancelled", "unknown"} {
		t.Run(state, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			old := database.DB
			database.DB = db
			defer func() { database.DB = old }()
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*tasks.*FOR UPDATE`).WithArgs("task", 1).WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "status", "version"}).AddRow("id", "task", state, 2))
			allowed := state == "queued" || state == "failed"
			if allowed {
				mock.ExpectExec(`UPDATE "tasks" SET "internal_id"=\$1,"pipeline"=\$2,"remark"=\$3,"updated_at"=\$4,"version"=\$5 WHERE`).WithArgs("new", "pipeline", "remark", sqlmock.AnyArg(), 3, "id").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			task, err := (&TaskService{}).UpdateTask(context.Background(), "task", &model.TaskUpdateRequest{InternalID: "new", Pipeline: "pipeline", Remark: "remark"})
			if allowed {
				if err != nil || task.Status != model.TaskStatus(state) || task.Version != 3 {
					t.Fatal("editable update failed", err)
				}
			} else if err == nil {
				t.Fatal("non-editable task changed")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestTaskEditingKeepsExecutionAndInterpretationLocks(t *testing.T) {
	now := time.Now()
	for _, task := range []model.Task{
		{Status: model.TaskStatusFailed, Executor: model.ExecutorCVM, ExecutionPhase: "terminating"},
		{Status: model.TaskStatusQueued, Executor: model.ExecutorCVM, ExecutionPhase: "provisioning"},
		{Status: model.TaskStatusFailed, InterpretationCompletedAt: &now},
	} {
		if validateTaskEditState(&task) == nil {
			t.Fatal("execution or interpretation lock bypassed")
		}
	}
}
