package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/bioinfo/schema-platform/internal/sepiida"
	"github.com/google/uuid"
)

type TaskService struct {
	cfg     *config.Config
	sepiida *sepiida.Client
	repo    *repository.TaskRepository
	mu      sync.RWMutex
	running map[string]*exec.Cmd
}

func NewTaskService(cfg *config.Config) *TaskService {
	var sepClient *sepiida.Client
	if cfg.Sepiida.Enabled && cfg.Sepiida.QueryKey != "" {
		sepClient = sepiida.NewClient(cfg.Sepiida.ServerURL, cfg.Sepiida.QueryKey)
	}

	return &TaskService{
		cfg:     cfg,
		sepiida: sepClient,
		repo:    repository.NewTaskRepository(),
		running: make(map[string]*exec.Cmd),
	}
}

// StartSepiidaSync begins periodic syncing of task status from Sepiida.
// Should be called once at server startup.
func (s *TaskService) StartSepiidaSync(ctx context.Context, interval time.Duration) {
	if s.sepiida == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.syncRunningTaskStatuses()
			}
		}
	}()
}

// syncRunningTaskStatuses queries Sepiida for all "running" tasks in Octopus DB
// and updates their status based on Sepiida's authoritative workflow state.
func (s *TaskService) syncRunningTaskStatuses() {
	tasks, err := s.repo.FindByStatus(model.TaskStatusRunning)
	if err != nil {
		return
	}

	for _, task := range tasks {
		s.syncTaskFromSepiida(&task)
	}
}

func (s *TaskService) syncTaskFromSepiida(task *model.Task) {
	if s.sepiida == nil || task.UUID == "" {
		return
	}

	workflow, _, err := s.sepiida.GetWorkflowWithTasks(task.UUID)
	if err != nil || workflow == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	switch workflow.Status {
	case model.SepiidaStatusRunning:
		if task.Status != model.TaskStatusRunning {
			task.Status = model.TaskStatusRunning
			changed = true
		}
	case model.SepiidaStatusSuccess:
		if task.Status != model.TaskStatusCompleted {
			task.Status = model.TaskStatusCompleted
			task.Progress = 100
			task.Error = ""
			now := time.Now()
			task.FinishedAt = &now
			changed = true
		}
	case model.SepiidaStatusFailed:
		if task.Status != model.TaskStatusFailed {
			task.Status = model.TaskStatusFailed
			task.Error = "Workflow failed (reported by Sepiida)"
			now := time.Now()
			task.FinishedAt = &now
			changed = true
		}
	case model.SepiidaStatusCancelled:
		if task.Status != model.TaskStatusCancelled {
			task.Status = model.TaskStatusCancelled
			now := time.Now()
			task.FinishedAt = &now
			changed = true
		}
	}

	if changed {
		task.UpdatedAt = time.Now()
		s.repo.Update(task)
	}
}

// getExecutorPath returns the miniwdl executable path based on executor type
func (s *TaskService) getExecutorPath(executor model.ExecutorType) string {
	switch executor {
	case model.ExecutorSlurm:
		return s.cfg.Task.MiniWDLSlurmPath
	case model.ExecutorLSF:
		return s.cfg.Task.MiniWDLLSFPath
	default:
		return s.cfg.Task.MiniWDLPath
	}
}

// getConfigFile returns the config file path based on executor type
func (s *TaskService) getConfigFile(executor model.ExecutorType, customConfig string) string {
	if customConfig != "" {
		return customConfig
	}

	var cfgName string
	switch executor {
	case model.ExecutorSlurm:
		cfgName = "slurm.cfg"
	case model.ExecutorLSF:
		cfgName = "lsf.cfg"
	default:
		cfgName = "local.cfg"
	}

	return filepath.Join(s.cfg.Task.TemplateDir, "conf", cfgName)
}

// CreateTask creates a new task (status: queued)
func (s *TaskService) CreateTask(ctx context.Context, req *model.TaskCreateRequest, userID uint) (*model.Task, error) {
	executor := req.Executor
	if executor == "" {
		executor = model.ExecutorType(s.cfg.Task.DefaultExecutor)
	}

	workflowUUID := uuid.New().String()
	taskID := workflowUUID[:8]

	var inputJSONStr string
	if req.Inputs != nil {
		inputJSON, err := json.Marshal(req.Inputs)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal inputs: %w", err)
		}
		inputJSONStr = string(inputJSON)
	}

	outputDir := req.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(s.cfg.Task.OutputDir, workflowUUID)
	}

	configFile := s.getConfigFile(executor, req.ConfigFile)

	now := time.Now()
	task := &model.Task{
		ID:              taskID,
		UUID:            workflowUUID,
		Name:            req.PipelineName,
		SampleID:        req.SampleID,
		InternalID:      req.InternalID,
		Pipeline:        req.PipelineName,
		PipelineVersion: req.PipelineVersion,
		Template:        req.Template,
		Executor:        executor,
		InputJSON:       inputJSONStr,
		ConfigFile:      configFile,
		OutputDir:       outputDir,
		Status:          model.TaskStatusQueued,
		Progress:        0,
		Remark:          req.Remark,
		CreatedBy:       userID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.Create(task); err != nil {
		return nil, fmt.Errorf("failed to save task: %w", err)
	}

	return task, nil
}

// StartTask starts a queued or failed task
func (s *TaskService) StartTask(ctx context.Context, id string) (*model.Task, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusFailed {
		return nil, fmt.Errorf("task cannot be started from status: %s", task.Status)
	}

	task.Status = model.TaskStatusRunning
	task.Progress = 0
	task.Error = ""
	now := time.Now()
	task.StartedAt = &now
	task.UpdatedAt = now

	if err := s.repo.Update(task); err != nil {
		return nil, err
	}

	// Fire-and-forget: launch miniwdl in background, Sepiida Agent monitors it
	go s.launchTask(task)

	return task, nil
}

// StopTask stops a running task
func (s *TaskService) StopTask(ctx context.Context, id string) (*model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusRunning {
		return nil, fmt.Errorf("task is not running")
	}

	if cmd, ok := s.running[id]; ok {
		cmd.Process.Kill()
		delete(s.running, id)
	}

	task.Status = model.TaskStatusQueued
	task.Progress = 0
	now := time.Now()
	task.UpdatedAt = now

	if err := s.repo.Update(task); err != nil {
		return nil, err
	}

	return task, nil
}

// RetryTask retries a failed task
func (s *TaskService) RetryTask(ctx context.Context, id string) (*model.Task, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusFailed {
		return nil, fmt.Errorf("only failed tasks can be retried")
	}

	task.Status = model.TaskStatusRunning
	task.Progress = 0
	task.Error = ""
	now := time.Now()
	task.StartedAt = &now
	task.UpdatedAt = now

	if err := s.repo.Update(task); err != nil {
		return nil, err
	}

	go s.launchTask(task)

	return task, nil
}

// launchTask starts miniwdl as a detached process and returns immediately.
// Sepiida Agent monitors the filesystem and reports progress to Sepiida Server.
// The syncLoop periodically updates Octopus DB from Sepiida Server.
func (s *TaskService) launchTask(task *model.Task) {
	executorPath := s.getExecutorPath(task.Executor)
	wdlPath := filepath.Join(s.cfg.Task.TemplateDir, task.Template+".wdl")

	// Write input JSON
	inputFile := filepath.Join(s.cfg.Task.OutputDir, task.ID+"_inputs.json")
	if err := os.WriteFile(inputFile, []byte(task.InputJSON), 0644); err != nil {
		s.updateTaskError(task, fmt.Sprintf("Failed to write input file: %v", err))
		return
	}

	// Create log directory
	logPath := filepath.Join(s.cfg.Task.OutputDir, task.UUID, "octopus.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)

	// Build command
	cmd := exec.Command(
		executorPath, "run",
		wdlPath,
		"-p", s.cfg.Task.TemplateDir,
		"--cfg", task.ConfigFile,
		"-i", inputFile,
		"-d", task.UUID,
	)

	// Detach from parent process
	cmd.SysProcAttr = &syscall.SysProcAttr{}

	logFile, err := os.Create(logPath)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	// Start (non-blocking) — returns immediately
	if err := cmd.Start(); err != nil {
		s.updateTaskError(task, fmt.Sprintf("Failed to start miniwdl: %v", err))
		return
	}

	task.LogPath = logPath
	s.repo.Update(task)

	s.mu.Lock()
	s.running[task.ID] = cmd
	s.mu.Unlock()

	// Background wait: reap the process when it exits.
	// This is a fallback — Sepiida Agent is the authoritative status source.
	go func() {
		err := cmd.Wait()

		os.Remove(inputFile)

		s.mu.Lock()
		delete(s.running, task.ID)
		s.mu.Unlock()

		// Only update status if Sepiida hasn't already done so via sync loop.
		// Check current DB status first.
		current, dbErr := s.repo.FindByUUID(task.UUID)
		if dbErr == nil && (current.Status == model.TaskStatusCompleted ||
			current.Status == model.TaskStatusFailed ||
			current.Status == model.TaskStatusCancelled) {
			return // Sepiida already handled it
		}

		finishedAt := time.Now()
		if err != nil {
			task.Status = model.TaskStatusFailed
			task.Error = err.Error()
		} else {
			task.Status = model.TaskStatusCompleted
			task.Progress = 100
		}
		task.FinishedAt = &finishedAt
		task.UpdatedAt = finishedAt
		s.repo.Update(task)
	}()
}

func (s *TaskService) updateTaskError(task *model.Task, errMsg string) {
	s.mu.Lock()
	task.Status = model.TaskStatusFailed
	task.Error = errMsg
	task.UpdatedAt = time.Now()
	s.repo.Update(task)
	s.mu.Unlock()
}

// GetTask retrieves a task by UUID
func (s *TaskService) GetTask(ctx context.Context, id string) (*model.Task, error) {
	return s.repo.FindByUUID(id)
}

// GetTaskProgress retrieves task with Sepiida progress
func (s *TaskService) GetTaskProgress(ctx context.Context, id string) (*model.TaskProgressResponse, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	resp := &model.TaskProgressResponse{
		ID:        task.UUID,
		UUID:      task.UUID,
		Name:      task.Name,
		Template:  task.Template,
		Status:    task.Status,
		Progress:  task.Progress,
		CreatedAt: task.CreatedAt,
	}

	// Query Sepiida for real-time progress
	if s.sepiida != nil && task.UUID != "" {
		workflow, tasks, err := s.sepiida.GetWorkflowWithTasks(task.UUID)
		if err == nil && workflow != nil {
			resp.Sepiida = workflow
			resp.Tasks = tasks
			s.syncTaskFromSepiida(task)
		}
	}

	return resp, nil
}

// ListTasks lists tasks with optional filtering
func (s *TaskService) ListTasks(ctx context.Context, query *model.TaskListQuery) (*model.TaskListResponse, error) {
	tasks, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.TaskResponse, len(tasks))
	for i, task := range tasks {
		items[i] = task.ToResponse()
	}

	return &model.TaskListResponse{
		Total: total,
		Items: items,
	}, nil
}

// CancelTask cancels a running task
func (s *TaskService) CancelTask(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusRunning && task.Status != model.TaskStatusQueued {
		return fmt.Errorf("task is not running or queued")
	}

	if cmd, ok := s.running[id]; ok {
		cmd.Process.Kill()
	}

	task.Status = model.TaskStatusCancelled
	now := time.Now()
	task.FinishedAt = &now
	task.UpdatedAt = now

	return s.repo.Update(task)
}

// UpdateTask updates task fields
func (s *TaskService) UpdateTask(ctx context.Context, id string, req *model.TaskUpdateRequest) (*model.Task, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if task.Status == model.TaskStatusRunning {
		return nil, fmt.Errorf("cannot edit a running task")
	}

	if req.InternalID != "" {
		task.InternalID = req.InternalID
	}
	if req.Pipeline != "" {
		task.Pipeline = req.Pipeline
	}
	if req.Remark != "" {
		task.Remark = req.Remark
	}
	task.UpdatedAt = time.Now()

	if err := s.repo.Update(task); err != nil {
		return nil, err
	}

	return task, nil
}

// GetTaskLogs retrieves task logs
func (s *TaskService) GetTaskLogs(ctx context.Context, id string) (string, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return "", fmt.Errorf("task not found: %s", id)
	}

	if task.LogPath != "" {
		data, err := os.ReadFile(task.LogPath)
		if err == nil {
			return string(data), nil
		}
	}

	if task.UUID != "" {
		lastLink := filepath.Join(s.cfg.Task.OutputDir, task.UUID, "_LAST")
		if target, err := os.Readlink(lastLink); err == nil {
			workflowLog := filepath.Join(s.cfg.Task.OutputDir, task.UUID, target, "workflow.log")
			data, err := os.ReadFile(workflowLog)
			if err == nil {
				return string(data), nil
			}
		}
	}

	return "", fmt.Errorf("no log file available")
}
