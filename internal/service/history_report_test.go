package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

func TestHistoryGroupingUsesCoordinatesNotGeneOrHGVS(t *testing.T) {
	a := map[string]interface{}{"chromosome": "chr1", "position": "100", "ref": "A", "alt": "G", "gene": "SAME", "hgvsc": "same"}
	b := map[string]interface{}{"chromosome": "1", "position": "101", "ref": "A", "alt": "G", "gene": "SAME", "hgvsc": "same"}
	first, known := historyGroupIdentity("snv-indel", "hg19", "a", a)
	if !known {
		t.Fatal("complete identity rejected")
	}
	second, _ := historyGroupIdentity("snv-indel", "hg19", "b", b)
	if first == second {
		t.Fatal("different positions merged")
	}
	otherBuild, _ := historyGroupIdentity("snv-indel", "hg38", "a", a)
	if first == otherBuild {
		t.Fatal("different references merged")
	}
	a["chromosome"] = "1"
	same, _ := historyGroupIdentity("snv-indel", "hg19", "c", a)
	if first != same {
		t.Fatal("contig alias not normalized")
	}
	u1, known := historyGroupIdentity("snv-indel", "", "a", a)
	u2, _ := historyGroupIdentity("snv-indel", "", "b", a)
	if known || u1 == u2 {
		t.Fatal("unknown reference merged")
	}
}

func TestHistoryDoesNotMergeTruncatedAlleles(t *testing.T) {
	fields := compactHistoryFields(map[string]interface{}{"Chromosome": "chr1", "Position": "100", "Ref": strings.Repeat("A", 2100), "Alt": "G"}, "snv-indel")
	_, known := historyGroupIdentity("snv-indel", "reference", "source", fields)
	if known {
		t.Fatal("truncated allele accepted as an exact identity")
	}
}

func TestHistoryReferenceIdentityIgnoresSignedURLAndWorkflowAlias(t *testing.T) {
	_, a := historyReferenceIdentity(`{"reference_genome":"hg19","SingleWES.fasta":"https://reference.example/hg19.fa?signature=private"}`)
	_, b := historyReferenceIdentity(`{"reference_genome":"hg19","TrioWES.fasta":"https://reference.example/hg19.fa?signature=other"}`)
	if a == "" || a != b {
		t.Fatal("same FASTA split by workflow or authorization")
	}
	_, unknown := historyReferenceIdentity(`{"reference_genome":"hg19"}`)
	if unknown != "" {
		t.Fatal("assembly guessed as exact sequence identity")
	}
}

func TestHistoryFailureRollsBackAdjustmentAndAudit(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	d := model.ResultDataset{ID: strings.Repeat("d", 64), TenantID: "org:a", TaskUUID: "task", ExecutionAttemptID: "attempt", Table: "snv-indel", DataVersion: "hash"}
	saved := model.ResultRowAdjustment{TenantID: d.TenantID, TaskUUID: d.TaskUUID, ExecutionAttemptID: d.ExecutionAttemptID, Table: d.Table, RowID: strings.Repeat("a", 64), DatasetVersion: d.DataVersion, Version: 1, PayloadJSON: `{"reported":true,"acmgOverride":"Pathogenic"}`}
	event := model.ResultRowAdjustmentEvent{ID: "event", AfterJSON: saved.PayloadJSON, BeforeJSON: "{}", CreatedAt: time.Now().UTC()}
	source := model.HistoryReport{ID: historyReportID(&d, saved.RowID), TenantID: d.TenantID, TaskUUID: d.TaskUUID, AttemptID: d.ExecutionAttemptID, DatasetID: d.ID, RowID: saved.RowID, FieldsJSON: "{}"}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "result_row_adjustments"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "result_row_adjustment_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "history_reports"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO "history_scope_revisions"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "history_scope_revisions".*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "revision"}).AddRow(d.TenantID, 1))
	mock.ExpectExec(`UPDATE "history_scope_revisions"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "history_reports"`).WillReturnError(errors.New("index write failed"))
	mock.ExpectRollback()
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := persistParquetAdjustment(tx, &saved, 0); err != nil {
			return err
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return persistHistoryReport(tx, &model.Task{UUID: "task", ExecutionAttemptID: "attempt"}, &d, &saved, &event, &source)
	})
	if err == nil {
		t.Fatal("projection failure silently committed")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryVersionSwitchPreservesReportedSnapshot(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	d := model.ResultDataset{ID: "dataset", TenantID: "org:a", TaskUUID: "task", ExecutionAttemptID: "attempt", Table: "snv-indel"}
	saved := model.ResultRowAdjustment{RowID: strings.Repeat("a", 64), Version: 2, PayloadJSON: `{"reported":true,"activeAcmgVersion":"svcv4","svcv4Assessment":{"result":{"classification":"VUS","vusSubclass":"VUS-high"}}}`}
	event := model.ResultRowAdjustmentEvent{CreatedAt: time.Now().UTC(), BeforeJSON: `{"reported":true}`}
	var projected *model.HistoryReport
	if err := db.Callback().Update().Before("gorm:update").Register("test:history_snapshot", func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*model.HistoryReport); ok {
			copy := *row
			projected = &copy
		}
	}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "history_reports"`).WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "revision", "reported", "classification", "acmg_version", "reported_classification", "reported_acmg_version"}).AddRow(historyReportID(&d, saved.RowID), d.TenantID, 1, true, "Benign", "legacy", "Benign", "legacy"))
	mock.ExpectExec(`INSERT INTO "history_scope_revisions"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "history_scope_revisions".*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "revision"}).AddRow(d.TenantID, 1))
	mock.ExpectExec(`UPDATE "history_scope_revisions"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "history_reports"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	err := db.Transaction(func(tx *gorm.DB) error { return persistHistoryReport(tx, &model.Task{}, &d, &saved, &event, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if projected == nil || projected.ACMGVersion != "svcv4" || projected.Classification != "VUS" || projected.VusSubclass != "VUS-high" || projected.ReportedACMGVersion != "legacy" || projected.ReportedClassification != "Benign" || projected.ReportedVusSubclass != "" {
		t.Fatalf("changed reported snapshot: %#v", projected)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHistorySyncUsesStableRevisionAndIDPagination(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	mock.ExpectQuery(`SELECT \* FROM "history_scope_revisions" WHERE tenant_id=\$1`).WithArgs("org:a", 1).WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "revision"}).AddRow("org:a", 8))
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	mock.ExpectQuery(`SELECT \* FROM "history_reports" WHERE \(tenant_id=\$1 AND "table"=\$2 AND revision<=\$3\).*ORDER BY revision ASC,id ASC LIMIT \$7`).WithArgs("org:a", "roh", uint64(8), uint64(0), uint64(0), "", 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "revision", "deleted"}).AddRow(a, 8, true).AddRow(b, 8, true))
	result, err := SyncHistoryReports(context.Background(), "org:a", "roh", "", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || result.Cursor != "8:"+a || len(result.Rows) != 1 {
		t.Fatalf("lost same-revision pagination: %#v", result)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryCompletedCursorDoesNotUseDatabaseSentinelCollation(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	mock.ExpectQuery(`SELECT \* FROM "history_scope_revisions" WHERE tenant_id=\$1`).WithArgs("org:a", 1).WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "revision"}).AddRow("org:a", 8))
	mock.ExpectQuery(`SELECT \* FROM "history_reports" WHERE \(tenant_id=\$1 AND "table"=\$2 AND revision<=\$3\) AND revision>\$4.*LIMIT \$5`).WithArgs("org:a", "roh", uint64(8), uint64(8), 2).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	result, err := SyncHistoryReports(context.Background(), "org:a", "roh", "8:~", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 0 || !result.Complete {
		t.Fatal("completed cursor replayed rows")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryDetailRejectsAnotherTenantBeforeReadingEvents(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	id := strings.Repeat("a", 64)
	mock.ExpectQuery(`SELECT \* FROM "history_reports" WHERE id=\$1 AND tenant_id=\$2 AND deleted=false`).WithArgs(id, "org:other", 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, _, err := HistoryReportDetail(context.Background(), "org:other", id); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cross tenant history accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryDetailOnlyLinksCurrentAttemptAndDataset(t *testing.T) {
	for _, tc := range []struct {
		name, attempt string
		count         int64
		current       bool
	}{
		{"current", "attempt-a", 1, true}, {"old attempt", "attempt-b", 0, false}, {"replaced dataset", "attempt-a", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newUploadTransactionTestDB(t)
			previous := database.DB
			database.DB = db
			t.Cleanup(func() { database.DB = previous })
			id := strings.Repeat("a", 64)
			mock.ExpectQuery(`SELECT \* FROM "history_reports"`).WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "task_uuid", "attempt_id", "dataset_id", "dataset_version"}).AddRow(id, "org:a", "task-a", "attempt-a", "dataset-a", "version-a"))
			mock.ExpectQuery(`SELECT \* FROM "tasks"`).WillReturnRows(sqlmock.NewRows([]string{"uuid", "tenant_id", "execution_attempt_id"}).AddRow("task-a", "org:a", tc.attempt))
			if tc.attempt == "attempt-a" {
				mock.ExpectQuery(`SELECT count\(\*\) FROM "result_datasets"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.count))
			}
			mock.ExpectQuery(`SELECT \* FROM "result_row_adjustment_events"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			row, _, err := HistoryReportDetail(context.Background(), "org:a", id)
			if err != nil {
				t.Fatal(err)
			}
			if row.CurrentSource == nil || *row.CurrentSource != tc.current {
				t.Fatalf("unexpected source availability: %v", row.CurrentSource)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHistoryCompactSourcePreservesOriginalParquetFields(t *testing.T) {
	input := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(input, []byte("Chromosome\tPosition\tRef\tAlt\tGene\tGnomAD_AF\tAlphaMissense_AMC\nchr1\t100\tA\tG\tTEST\t0.0001\tlongannotation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, _, _, err := convertArchivedTextParquet(input)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file)
	page, err := NewParquetReader().ReadPage(file, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	fields := compactHistoryFields(page.Rows[0], "snv-indel")
	if fields["chromosome"] != "chr1" || fields["position"] != "100" || fields["gnomadAF"] != "0.0001" {
		t.Fatalf("source fields lost: %#v", fields)
	}
	if _, ok := fields["alphaMissenseAMC"]; ok {
		t.Fatal("full annotation duplicated")
	}
}

func TestHistoryTombstonesExposeOnlyRemovalIdentity(t *testing.T) {
	encoded, err := json.Marshal(model.HistoryReport{ID: strings.Repeat("a", 64), Revision: 2, Deleted: true, TaskUUID: "private-task", FieldsJSON: `{"gene":"private"}`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "taskUuid") {
		t.Fatal("tombstone exposes deleted data")
	}
}

func TestHistoryCursorRejectsMalformedAndAcceptsCompletedWatermark(t *testing.T) {
	for _, cursor := range []string{"-1:~", "5:", "5:../../path", "bad", "5:~:x"} {
		if _, _, err := parseHistoryCursor(cursor); err == nil {
			t.Fatalf("accepted %q", cursor)
		}
	}
	if n, id, err := parseHistoryCursor("45:~"); err != nil || n != 45 || id != "~" {
		t.Fatal("completed cursor rejected")
	}
}
