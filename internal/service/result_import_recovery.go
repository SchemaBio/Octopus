package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
)

type ArchivedResultImportCheck struct {
	TaskUUID             string                   `json:"task_uuid"`
	AttemptID            string                   `json:"attempt_id"`
	TaskStatus           model.TaskStatus         `json:"task_status"`
	ImportStatus         model.ResultImportStatus `json:"import_status"`
	ImportAttempts       int                      `json:"import_attempts"`
	ArchiveDirectory     string                   `json:"archive_directory"`
	OutputsManifestReady bool                     `json:"outputs_manifest_ready"`
}

// InspectArchivedTaskResults validates an operator-selected task attempt
// without changing task state or creating an import batch.
func (s *TaskService) InspectArchivedTaskResults(taskUUID, attemptID string) (*ArchivedResultImportCheck, error) {
	taskUUID = strings.TrimSpace(taskUUID)
	attemptID = strings.TrimSpace(attemptID)
	if _, err := uuid.Parse(taskUUID); err != nil {
		return nil, fmt.Errorf("task must be a valid UUID")
	}
	if _, err := uuid.Parse(attemptID); err != nil {
		return nil, fmt.Errorf("attempt must be a valid UUID")
	}
	task, err := s.repo.FindByUUID(taskUUID)
	if err != nil || task == nil {
		return nil, fmt.Errorf("task not found")
	}
	if task.Executor != model.ExecutorCVM {
		return nil, fmt.Errorf("task is not a CVM task")
	}
	if task.Status != model.TaskStatusCompleted {
		return nil, fmt.Errorf("task is not completed")
	}
	if task.ExecutionAttemptID != attemptID {
		return nil, fmt.Errorf("attempt does not match the current task execution")
	}
	if strings.TrimSpace(s.cfg.Task.ArchiveDir) == "" {
		return nil, fmt.Errorf("archive directory is not configured")
	}
	archiveDir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(archiveDir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("archive directory is unavailable for the selected attempt")
	}
	outputsInfo, err := os.Stat(filepath.Join(archiveDir, "outputs.resolved.json"))
	if err != nil || !outputsInfo.Mode().IsRegular() || outputsInfo.Size() == 0 {
		return nil, fmt.Errorf("resolved outputs manifest is unavailable for the selected attempt")
	}
	return &ArchivedResultImportCheck{
		TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID,
		TaskStatus: task.Status, ImportStatus: task.ResultImportStatus,
		ImportAttempts: task.ResultImportAttempts, ArchiveDirectory: archiveDir,
		OutputsManifestReady: true,
	}, nil
}

// RecoverArchivedTaskResults imports one explicitly selected current attempt.
// Import ownership is checked again under the database row lock.
func (s *TaskService) RecoverArchivedTaskResults(ctx context.Context, taskUUID, attemptID string) (*model.TaskProgressResponse, error) {
	check, err := s.InspectArchivedTaskResults(taskUUID, attemptID)
	if err != nil {
		return nil, err
	}
	if check.ImportStatus == model.ResultImportStatusSuccess {
		return nil, fmt.Errorf("selected attempt already has a successful result import")
	}
	task, err := s.repo.FindByUUID(check.TaskUUID)
	if err != nil {
		return nil, fmt.Errorf("reload task for result import: %w", err)
	}
	if err := s.runTaskArchiveImportForAttempt(task, check.ArchiveDirectory, check.AttemptID, "operator"); err != nil {
		return nil, err
	}
	return s.GetTaskProgress(ctx, check.TaskUUID)
}
