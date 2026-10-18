package service

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestLocalRetryRejectsPreviousWorkflowReports(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "task")
	old := filepath.Join(base, "old-run")
	current := filepath.Join(base, "new-run")
	for _, dir := range []string{old, current} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	s := &TaskService{cfg: &config.Config{Task: config.TaskConfig{OutputDir: root}}}
	started := time.Now().Add(-time.Second)
	task := &model.Task{UUID: "task", Executor: model.ExecutorLocal, RetryStartedAt: &started}
	report := &model.SepiidaWorkflow{OutputDir: old, Status: model.SepiidaStatusFailed}
	if s.currentWorkflowReport(task, report) {
		t.Fatal("report must be ignored before a new run directory exists")
	}
	last := filepath.Join(base, "_LAST")
	if err := os.Symlink(old, last); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("MiniWDL symlink test requires Windows symlink privileges; run on Linux: %v", err)
		}
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	task.RetryStartedAt = &future
	if s.currentWorkflowReport(task, report) {
		t.Fatal("previous _LAST cannot belong to a retry that has not started")
	}
	if err := os.Remove(last); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(current, last); err != nil {
		t.Fatal(err)
	}
	task.RetryStartedAt = &started
	for _, status := range []model.SepiidaStatus{model.SepiidaStatusFailed, model.SepiidaStatusSuccess, model.SepiidaStatusRunning} {
		report.Status = status
		if s.currentWorkflowReport(task, report) {
			t.Fatalf("old %s report replaced the retry", status)
		}
		report.OutputDir = current
		if !s.currentWorkflowReport(task, report) {
			t.Fatalf("new %s report was rejected", status)
		}
		report.OutputDir = old
	}
	// CVM uses Agent attempt identity rather than local filesystem pointers.
	task.Executor = model.ExecutorCVM
	if !s.currentWorkflowReport(task, report) {
		t.Fatal("local retry gating must not change CVM behavior")
	}
}
