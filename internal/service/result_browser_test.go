package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestBrowserOrdinalProvesImmutableRowIdentity(t *testing.T) {
	d := &model.ResultDataset{ID: "dataset", ObjectSHA256: "hash", Rows: 10}
	h := sha256.Sum256([]byte("dataset/hash/0"))
	id := hex.EncodeToString(h[:])
	if !browserOrdinalMatches(d, id, 0) {
		t.Fatal("first row rejected")
	}
	for _, ordinal := range []int64{-1, 1, 10} {
		if browserOrdinalMatches(d, id, ordinal) {
			t.Fatal("forged ordinal accepted")
		}
	}
	d.ObjectSHA256 = "new-hash"
	if browserOrdinalMatches(d, id, 0) {
		t.Fatal("old object identity accepted")
	}
}

func TestBrowserSnapshotLocksScopeAndUsesRevisionCursor(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	defer func() { database.DB = previous }()
	task := &model.Task{UUID: "task", ExecutionAttemptID: "attempt"}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "tasks".*FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"uuid", "execution_attempt_id"}).AddRow("task", "attempt"))
	mock.ExpectQuery(`SELECT .*FROM "result_datasets".*FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"id", "data_version", "adjustment_revision"}).AddRow("dataset", "hash", 8))
	mock.ExpectQuery(`SELECT .*FROM "result_row_adjustments".*revision>.*ORDER BY revision ASC`).WillReturnRows(sqlmock.NewRows([]string{"row_id", "revision", "payload_json", "version"}).AddRow("row", 8, `{"reviewed":true}`, 2))
	mock.ExpectCommit()
	cursor := uint64(7)
	snapshot, err := (&ResultService{}).BrowserAdjustments(context.Background(), task, "snv-indel", "attempt", "hash", &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 8 || len(snapshot.Items) != 1 || snapshot.Items[0].Version != 2 {
		t.Fatal("invalid delta")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestBrowserSnapshotRejectsOldAttemptWithoutQuery(t *testing.T) {
	_, err := (&ResultService{}).BrowserAdjustments(context.Background(), &model.Task{UUID: "task", ExecutionAttemptID: "new"}, "snv-indel", "old", "hash", nil)
	if !errors.Is(err, ErrAdjustmentConflict) {
		t.Fatal("accepted old attempt")
	}
}
