package service

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestCNVBaselineDeletion(t *testing.T) {
	for _, scenario := range []string{"success", "running", "pipeline", "active_task", "pair_failure"} {
		t.Run(scenario, func(t *testing.T) {
			_, mock := newAIEvaluatorScopeTest(t)
			mock.ExpectBegin()
			baselineRows := func() *sqlmock.Rows {
				return sqlmock.NewRows([]string{"id", "uuid", "task_uuid", "external_org_id"}).AddRow(7, "baseline-1", "calibration-1", "org-1")
			}
			mock.ExpectQuery(`SELECT .*cnv_baselines.*external_org_id`).WithArgs("baseline-1", "org-1", 1).WillReturnRows(baselineRows())
			status := "completed"
			if scenario == "running" {
				status = "running"
			}
			mock.ExpectQuery(`SELECT .*tasks.*FOR UPDATE`).WithArgs("calibration-1", 1).WillReturnRows(sqlmock.NewRows([]string{"uuid", "status"}).AddRow("calibration-1", status))
			if scenario != "running" {
				mock.ExpectQuery(`SELECT .*cnv_baselines.*FOR UPDATE`).WithArgs("baseline-1", "org-1", uint(7), 1).WillReturnRows(baselineRows())
				count := 0
				if scenario == "pipeline" {
					count = 1
				}
				mock.ExpectQuery(`SELECT count.*pipelines`).WithArgs(uint(7)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
				if scenario != "pipeline" {
					rows := sqlmock.NewRows([]string{"uuid", "pipeline_snapshot_json"})
					if scenario == "active_task" {
						rows.AddRow("analysis-1", `{"baseline_id":7,"baseline_uuid":"baseline-1"}`)
					}
					mock.ExpectQuery(`SELECT .*tasks.*status NOT IN`).WithArgs(model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusCancelled, model.TaskStatusPendingInterpretation).WillReturnRows(rows)
					if scenario != "active_task" {
						query := mock.ExpectExec(`DELETE FROM "cnv_baseline_read_pairs"`).WithArgs(uint(7))
						if scenario == "pair_failure" {
							query.WillReturnError(errors.New("storage failure"))
						} else {
							query.WillReturnResult(sqlmock.NewResult(0, 2))
							mock.ExpectExec(`DELETE FROM "cnv_baselines"`).WithArgs(uint(7)).WillReturnResult(sqlmock.NewResult(0, 1))
						}
					}
				}
			}
			if scenario == "success" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := deleteCNVBaseline(database.GetDB(), "baseline-1", model.OverlayActor{OrgID: "org-1", UserID: 42})
			if scenario == "success" && err != nil {
				t.Fatal(err)
			}
			if scenario != "success" && err == nil {
				t.Fatal("unsafe deletion accepted")
			}
			if scenario != "success" && scenario != "pair_failure" && !errors.Is(err, ErrCNVBaselineInUse) {
				t.Fatalf("expected conflict: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCNVBaselineDeletionScope(t *testing.T) {
	for _, actor := range []model.OverlayActor{{OrgID: "other-org", UserID: 42}, {UserID: 42}} {
		_, mock := newAIEvaluatorScopeTest(t)
		mock.ExpectBegin()
		if actor.OrgID != "" {
			mock.ExpectQuery(`SELECT .*cnv_baselines.*external_org_id`).WithArgs("baseline-1", actor.OrgID, 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		} else {
			mock.ExpectQuery(`SELECT .*cnv_baselines.*created_by`).WithArgs("baseline-1", actor.UserID, 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		}
		mock.ExpectRollback()
		if err := deleteCNVBaseline(database.GetDB(), "baseline-1", actor); !errors.Is(err, ErrCNVBaselineNotFound) {
			t.Fatalf("scope leaked: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	}
}
