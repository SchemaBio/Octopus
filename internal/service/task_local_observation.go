package service

import (
	"os"
	"path/filepath"

	"github.com/SchemaBio/Octopus/internal/model"
)

// currentWorkflowReport rejects the previous local execution while a retry is
// preparing or waiting for its first Agent report. Local Agent IDs are shared
// across attempts; CVM already filters reports by its per-attempt Agent ID.
func (s *TaskService) currentWorkflowReport(task *model.Task, report *model.SepiidaWorkflow) bool {
	if task.Executor == model.ExecutorCVM || task.RetryStartedAt == nil {
		return true
	}
	root := filepath.Join(s.cfg.Task.OutputDir, task.UUID)
	last := filepath.Join(root, "_LAST")
	info, err := os.Lstat(last)
	if err != nil || info.ModTime().Before(*task.RetryStartedAt) {
		return false
	}
	current, err := filepath.EvalSymlinks(last)
	if err != nil || ensurePathInsideBase(root, current) != nil {
		return false
	}
	reported, err := filepath.EvalSymlinks(report.OutputDir)
	if err != nil {
		return false
	}
	return filepath.Clean(current) == filepath.Clean(reported)
}
