package service

import (
	"testing"

	"github.com/bioinfo/schema-platform/internal/config"
)

func TestStatusManagerRejectsInvalidUpdates(t *testing.T) {
	mgr := NewStatusManager(&config.Config{Task: config.TaskConfig{ArchiveDir: t.TempDir()}})

	if err := mgr.UpdateStatus("task-1", []StatusUpdate{{Table: "../snv", RowIndex: 0}}); err == nil {
		t.Fatal("expected path-like table name to be rejected")
	}
	if err := mgr.UpdateStatus("task-1", []StatusUpdate{{Table: "snv", RowIndex: -1}}); err == nil {
		t.Fatal("expected negative row_index to be rejected")
	}
	if err := mgr.UpdateStatus("task-1", []StatusUpdate{{Table: "snv", RowIndex: 0, ReviewStatus: "ok\nbad"}}); err == nil {
		t.Fatal("expected control characters in status to be rejected")
	}
}

func TestStatusManagerAcceptsSafeUpdate(t *testing.T) {
	mgr := NewStatusManager(&config.Config{Task: config.TaskConfig{ArchiveDir: t.TempDir()}})

	if err := mgr.UpdateStatus("task-1", []StatusUpdate{{Table: "snv_indel", RowIndex: 1, ReviewStatus: "reviewed"}}); err != nil {
		t.Fatalf("expected safe status update to pass: %v", err)
	}
	status, err := mgr.GetStatus("task-1")
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if status.Tables["snv_indel"] == nil || len(status.Tables["snv_indel"].Rows) != 1 {
		t.Fatalf("expected status row to be persisted, got %#v", status.Tables["snv_indel"])
	}
}
