package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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

// CreateTask creates a new task (initially queued)
func (s *TaskService) CreateTask(ctx context.Context, req *model.TaskCreateRequest, userID uint) (*model.Task, error) {
	// Determine executor (use request or default from config)
	executor := req.Executor
	if executor == "" {
		executor = model.ExecutorType(s.cfg.Task.DefaultExecutor)
	}

	// Generate workflow UUID
	workflowUUID := uuid.New().String()

	// Generate short task ID from first 8 chars of UUID
	taskID := workflowUUID[:8]

	// Prepare input JSON if provided
	var inputJSONStr string
	if req.Inputs != nil {
		inputJSON, err := json.Marshal(req.Inputs)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal inputs: %w", err)
		}
		inputJSONStr = string(inputJSON)
	}

	// Output directory
	outputDir := req.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(s.cfg.Task.OutputDir, workflowUUID)
	}

	// Determine config file based on executor
	configFile := s.getConfigFile(executor, req.ConfigFile)

	// Create task record
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

	// Save task to database
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

	// Start execution in background
	go s.executeTask(task)

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

	// Kill process if running
	if cmd, ok := s.running[id]; ok {
		if err := cmd.Process.Kill(); err != nil {
			return nil, fmt.Errorf("failed to kill process: %w", err)
		}
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

	// Start execution in background
	go s.executeTask(task)

	return task, nil
}

// executeTask executes the miniwdl command
func (s *TaskService) executeTask(task *model.Task) {
	s.mu.Lock()
	task.Status = model.TaskStatusRunning
	now := time.Now()
	task.StartedAt = &now
	task.Progress = 0
	s.repo.Update(task)
	s.mu.Unlock()

	// Get executor path based on executor type
	executorPath := s.getExecutorPath(task.Executor)

	// Prepare WDL path
	wdlPath := filepath.Join(s.cfg.Task.TemplateDir, task.Template+".wdl")

	// Write input JSON to temp file
	inputFile := filepath.Join(s.cfg.Task.OutputDir, task.ID+"_inputs.json")
	if err := os.WriteFile(inputFile, []byte(task.InputJSON), 0644); err != nil {
		s.updateTaskError(task, fmt.Sprintf("Failed to write input file: %v", err))
		return
	}

	// Create log file for Octopus
	logPath := filepath.Join(s.cfg.Task.OutputDir, task.UUID, "octopus.log")

	// Build miniwdl command with -d uuid mode
	cmd := exec.CommandContext(context.Background(),
		executorPath, "run",
		wdlPath,
		"-p", s.cfg.Task.TemplateDir,
		"--cfg", task.ConfigFile,
		"-i", inputFile,
		"-d", task.UUID,
	)

	// Create parent directory for log
	os.MkdirAll(filepath.Dir(logPath), 0755)
	logFile, err := os.Create(logPath)
	if err != nil {
		logFile = os.Stdout
	} else {
		defer logFile.Close()
	}

	task.LogPath = logPath
	s.repo.Update(task)

	// Setup log output
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Store command for cancellation
	s.mu.Lock()
	s.running[task.ID] = cmd
	s.mu.Unlock()

	// Execute command
	err = cmd.Run()

	// Cleanup input file
	os.Remove(inputFile)

	// Cleanup running map
	s.mu.Lock()
	delete(s.running, task.ID)
	s.mu.Unlock()

	// Update status
	finishedAt := time.Now()
	task.FinishedAt = &finishedAt
	task.UpdatedAt = finishedAt

	if err != nil {
		task.Status = model.TaskStatusFailed
		task.Error = err.Error()
	} else {
		task.Status = model.TaskStatusCompleted
		task.Progress = 100
		// Note: archiving and parquet generation are handled by Sepiida Agent
	}

	s.repo.Update(task)
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
			s.updateStatusFromSepiida(task, workflow)
		}
	}

	return resp, nil
}

// updateStatusFromSepiida updates task status based on Sepiida workflow status
func (s *TaskService) updateStatusFromSepiida(task *model.Task, workflow *model.SepiidaWorkflow) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch workflow.Status {
	case model.SepiidaStatusRunning:
		task.Status = model.TaskStatusRunning
	case model.SepiidaStatusSuccess:
		task.Status = model.TaskStatusCompleted
	case model.SepiidaStatusFailed:
		task.Status = model.TaskStatusFailed
	case model.SepiidaStatusCancelled:
		task.Status = model.TaskStatusCancelled
	}
	task.UpdatedAt = time.Now()
	s.repo.Update(task)
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
		Total: int(total),
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
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
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
