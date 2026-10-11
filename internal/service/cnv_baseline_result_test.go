package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestBaselineOutputSelectsFinalReferenceOnly(t *testing.T) {
	for _, raw := range []string{
		`{"CNVBaselineFix.reference":"/out/final.cnn","CNVBaselineFix.new_batch_reference":"/out/batch.cnn","CNVBaselineFix.new_target_coverages":["/out/coverage.cnn"]}`,
		`{"outputs":{"CNVBaselineFix.reference":"/out/final.cnn","CNVBaselineFix.new_batch_reference":"/out/batch.cnn"}}`,
	} {
		if got := baselineOutputFromJSON(raw); got != "/out/final.cnn" {
			t.Fatalf("final reference = %q", got)
		}
	}
	if got := baselineOutputFromJSON(`{"CNVBaselineFix.new_batch_reference":"/out/batch.cnn"}`); got != "" {
		t.Fatalf("selected intermediate: %s", got)
	}
}

func TestBaselineRegistrationRejectsOldAttemptsAndRollsBackMissingBaseline(t *testing.T) {
	for _, tc := range []struct {
		name              string
		current, baseline bool
	}{
		{"current", true, true}, {"stale attempt", false, true}, {"deleted baseline", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			previous := database.DB
			database.DB = db
			defer func() { database.DB = previous }()
			mock.ExpectBegin()
			rows := sqlmock.NewRows([]string{"uuid", "execution_attempt_id"})
			if tc.current {
				rows.AddRow("task", "attempt")
			}
			mock.ExpectQuery(`SELECT .* FROM "tasks" .*execution_attempt_id.*FOR UPDATE`).WithArgs("task", "attempt", 1).WillReturnRows(rows)
			if tc.current {
				count := int64(0)
				if tc.baseline {
					count = 1
				}
				mock.ExpectExec(`UPDATE "cnv_baselines" SET`).WillReturnResult(sqlmock.NewResult(0, count))
			}
			if tc.current && tc.baseline {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err = (&TaskService{}).registerArchivedCNVBaseline(&model.Task{UUID: "task", ExecutionAttemptID: "attempt"}, "https://bucket/final.cnn")
			if (err == nil) != (tc.current && tc.baseline) {
				t.Fatalf("registration error=%v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBaselineArchiveRejectsMissingForeignEmptyAndRawNodeReferences(t *testing.T) {
	host := "bucket.cos.ap-guangzhou.myqcloud.com"
	prefix := "organizations/org/workflows/task/attempts/current"
	key := prefix + "/final.cnn"
	ref := "https://" + host + "/" + key
	for _, tc := range []struct {
		name, ref string
		objects   []s3ObjectInfo
		pass      bool
	}{
		{"valid", ref, []s3ObjectInfo{{Key: key, Size: 10}}, true},
		{"missing", ref, nil, false},
		{"empty", ref, []s3ObjectInfo{{Key: key, Size: 0}}, false},
		{"foreign attempt", "https://" + host + "/organizations/org/workflows/task/attempts/old/final.cnn", nil, false},
		{"foreign bucket", "https://other.example/" + key, nil, false},
		{"node path", "/mnt/data/output/final.cnn", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validatedCNVBaselineArchiveOutput(`{"outputs":{"CNVBaselineFix.reference":"`+tc.ref+`"}}`, prefix, host, tc.objects)
			if (err == nil) != tc.pass || (tc.pass && got != ref) {
				t.Fatalf("reference=%q error=%v", got, err)
			}
		})
	}
}

func TestBaselineImporterDoesNotRequireWESQC(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "outputs.resolved.json"), []byte(`{"CNVBaselineFix.reference":"https://bucket/final.cnn"}`), 0600); err != nil {
		t.Fatal(err)
	}
	imp := NewImporter(&config.Config{})
	imp.task = &model.Task{Template: "baseline_fix"}
	result, err := imp.importFromArchive("task", dir)
	if err != nil || !result.Success || result.Counts["cnv_baseline"] != 1 {
		t.Fatalf("baseline import=%#v error=%v", result, err)
	}
}
