package repository

import (
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"regexp"
	"testing"
)

func TestFocusWritesOnlyMarkerAndPreservesInterpretationSnapshot(t *testing.T) {
	for _, focused := range []bool{true, false} {
		t.Run(map[bool]string{true: "focus", false: "unfocus"}[focused], func(t *testing.T) {
			db, mock := newUploadRepositoryTestDB(t)
			repo := NewResultRepository()
			repo.db = db
			task := &model.Task{UUID: "task", TenantID: "org:tenant", ExecutionAttemptID: "attempt"}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT \* FROM "result_snv_indels"`).WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "tenant_id", "focused", "reviewed", "reviewed_by", "interpretation", "acmg_classification"}).AddRow("variant", "task", "org:tenant", !focused, true, "reviewer", "original interpretation", "Pathogenic"))
			// An exact SET list rejects any accidental review/evidence mutation.
			mock.ExpectExec(regexp.QuoteMeta(`UPDATE "result_snv_indels" SET "focused"=$1,"focused_at"=$2,"focused_by"=$3 WHERE id = $4 AND task_id = $5 AND tenant_id = $6`)).WithArgs(focused, sqlmock.AnyArg(), sqlmock.AnyArg(), "variant", "task", "org:tenant").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`INSERT INTO "variant_review_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			mutation, err := repo.FocusVariantWithEvent(task, "snv-indel", "variant", focused, 7, "actor@example.com")
			if err != nil {
				t.Fatal(err)
			}
			var snapshot map[string]interface{}
			if err := json.Unmarshal([]byte(mutation.Event.VariantSnapshotJSON), &snapshot); err != nil {
				t.Fatal(err)
			}
			review := snapshot["reviewStatus"].(map[string]interface{})
			if review["reviewed"] != true || review["reviewedBy"] != "reviewer" || snapshot["acmgClassification"] != "Pathogenic" {
				t.Fatalf("review or classification changed: %v", snapshot)
			}
			if !focused && (mutation.FocusedBy != "" || mutation.FocusedAt != nil) {
				t.Fatal("unfocus retained marker actor")
			}
			if mutation.Event.ActorEmail != "actor@example.com" {
				t.Fatal("operation actor lost")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
