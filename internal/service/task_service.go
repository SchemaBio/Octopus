package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/sepiida"
	"github.com/google/uuid"
)

type TaskService struct {
	cfg             *config.Config
	sepiida         *sepiida.Client
	overlay         *OverlayClient
	repo            *repository.TaskRepository
	importBatchRepo *repository.ResultImportBatchRepository
	uploadJobRepo   *repository.UploadJobRepository
	uploadFileRepo  *repository.UploadFileRepository
	assetRepo       *repository.DataAssetRepository
	sampleRepo      *repository.SampleRepository
	pipelineRepo    *repository.PipelineRepository
	mu              sync.RWMutex
	running         map[string]*exec.Cmd
}

const maxTaskLogBytes = 2 << 20

func (s *TaskService) estimateTaskMinutes(sampleID uint, uploadJob *model.UploadJob) int {
	if sampleID != 0 {
		var link model.SampleDataLink
		if err := database.GetDB().Where("sample_id = ?", sampleID).First(&link).Error; err == nil {
			read1, read1Err := s.assetRepo.FindByID(link.Read1AssetID)
			read2, read2Err := s.assetRepo.FindByID(link.Read2AssetID)
			if read1Err == nil && read2Err == nil &&
				read1.Status == model.FileStatusCompleted && read2.Status == model.FileStatusCompleted &&
				read1.FileSize > 0 && read2.FileSize > 0 {
				return estimateTaskMinutesFromBytes(read1.FileSize + read2.FileSize)
			}
		}
	}

	if uploadJob != nil && uploadJob.Status == model.UploadJobStatusCompleted {
		files, err := s.uploadFileRepo.FindByJobID(uploadJob.ID)
		if err == nil {
			var read1Bytes, read2Bytes int64
			for i := range files {
				if files[i].Status != model.FileStatusCompleted || files[i].FileSize <= 0 {
					continue
				}
				switch files[i].ReadType {
				case model.ReadTypeRead1:
					read1Bytes = files[i].FileSize
				case model.ReadTypeRead2:
					read2Bytes = files[i].FileSize
				}
			}
			if read1Bytes > 0 && read2Bytes > 0 {
				return estimateTaskMinutesFromBytes(read1Bytes + read2Bytes)
			}
		}
	}

	return defaultTaskEstimatedMinutes
}

func NewTaskService(cfg *config.Config) *TaskService {
	var sepClient *sepiida.Client
	if cfg.Sepiida.Enabled && cfg.Sepiida.QueryKey != "" {
		sepClient = sepiida.NewClient(cfg.Sepiida.ServerURL, cfg.Sepiida.QueryKey)
	}

	svc := &TaskService{
		cfg:             cfg,
		sepiida:         sepClient,
		overlay:         NewOverlayClient(cfg.Overlay),
		repo:            repository.NewTaskRepository(),
		importBatchRepo: repository.NewResultImportBatchRepository(),
		uploadJobRepo:   repository.NewUploadJobRepository(),
		uploadFileRepo:  repository.NewUploadFileRepository(),
		assetRepo:       repository.NewDataAssetRepository(),
		sampleRepo:      repository.NewSampleRepository(),
		pipelineRepo:    repository.NewPipelineRepository(),
		running:         make(map[string]*exec.Cmd),
	}

	return svc
}

func (s *TaskService) admitTask(ctx context.Context, action string, actor model.OverlayActor, task *model.Task) error {
	if s.overlay == nil {
		return nil
	}
	return s.overlay.AdmitTask(ctx, model.OverlayTaskAdmissionRequest{
		Action:      action,
		Actor:       actor,
		Task:        model.NewOverlayTaskSnapshot(task),
		RequestedAt: time.Now(),
	})
}

func (s *TaskService) emitTaskEvent(event string, actor model.OverlayActor, task *model.Task, previousStatus model.TaskStatus, message string) {
	if s.overlay == nil || task == nil {
		return
	}
	req := model.OverlayTaskEventRequest{
		Event:          event,
		Actor:          actor,
		Task:           model.NewOverlayTaskSnapshot(task),
		PreviousStatus: previousStatus,
		OccurredAt:     time.Now(),
		Message:        message,
	}
	go func() {
		if err := s.overlay.EmitTaskEvent(context.Background(), req); err != nil {
			fmt.Printf("WARNING: overlay task event %s failed for %s: %v\n", event, task.UUID, err)
		}
	}()
}

func (s *TaskService) emitStatusEvent(task *model.Task, previousStatus model.TaskStatus) {
	if task == nil || previousStatus == task.Status {
		return
	}
	event := ""
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusWaitingData:
		event = model.OverlayTaskEventQueued
	case model.TaskStatusRunning:
		event = model.OverlayTaskEventRunning
	case model.TaskStatusCompleted:
		event = model.OverlayTaskEventCompleted
	case model.TaskStatusFailed:
		event = model.OverlayTaskEventFailed
	case model.TaskStatusCancelled:
		event = model.OverlayTaskEventCancelled
	}
	if event != "" {
		s.emitTaskEvent(event, model.OverlayActor{}, task, previousStatus, task.Error)
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

	completedNow := false
	previousStatus := task.Status

	s.mu.Lock()
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
			completedNow = true
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
		s.emitStatusEvent(task, previousStatus)
	}
	s.mu.Unlock()

	if completedNow {
		s.importTaskArchive(task)
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

func validateTemplateName(name string) error {
	if name == "" {
		return fmt.Errorf("template is required")
	}
	if name != filepath.Base(name) || strings.Contains(name, `\`) || name == "." || name == ".." {
		return fmt.Errorf("invalid template name")
	}
	return nil
}

func ensurePathInsideBase(base, path string) error {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(baseAbs, pathAbs)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path escapes base directory")
	}
	return nil
}

func resolveRegularFileInsideBase(base, path string) (string, error) {
	baseEval, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	pathEval, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if err := ensurePathInsideBase(baseEval, pathEval); err != nil {
		return "", err
	}
	info, err := os.Stat(pathEval)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file is not regular")
	}
	return pathEval, nil
}

func readTaskLogFile(base, path string) (string, error) {
	safePath, err := resolveRegularFileInsideBase(base, path)
	if err != nil {
		return "", err
	}
	f, err := os.Open(safePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTaskLogBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxTaskLogBytes {
		return "", fmt.Errorf("log file exceeds maximum size of %d MB", maxTaskLogBytes>>20)
	}
	return string(data), nil
}

func actorCanUseOwnedResource(actor model.OverlayActor, ownerID uint) bool {
	if actor.Role == string(model.SystemRoleSuperAdmin) {
		return true
	}
	return ownerID != 0 && ownerID == actor.UserID
}

// CreateTask creates a new task. It resolves data file paths from the linked Sample,
// UploadJob, and Pipeline configuration, injecting them into WDL inputs.
// If the upload job is not yet completed, the task enters waiting_for_data status.
func (s *TaskService) CreateTask(ctx context.Context, req *model.TaskCreateRequest, actor model.OverlayActor) (*model.Task, error) {
	executor := model.ExecutorType(s.cfg.Task.DefaultExecutor)
	if executor != model.ExecutorLocal {
		executor = model.ExecutorLocal
	}
	if err := validateTemplateName(req.Template); err != nil {
		return nil, err
	}
	templatePath := filepath.Join(s.cfg.Task.TemplateDir, req.Template+".wdl")
	if err := ensurePathInsideBase(s.cfg.Task.TemplateDir, templatePath); err != nil {
		return nil, err
	}
	if _, err := os.Stat(templatePath); err != nil {
		return nil, fmt.Errorf("template not found: %s", req.Template)
	}

	workflowUUID := uuid.New().String()
	taskID := workflowUUID[:8]

	inputs := req.Inputs
	if inputs == nil {
		inputs = make(map[string]interface{})
	}
	if err := validateActorTaskFileInputs(s.cfg, actor, inputs); err != nil {
		return nil, err
	}

	var sampleIDRef uint
	var uploadJob *model.UploadJob
	needsStaging := false

	if req.SampleID != "" {
		sample, err := s.sampleRepo.FindScopedByUUID(req.SampleID, actor)
		if err != nil {
			return nil, fmt.Errorf("sample not found: %s", req.SampleID)
		}
		sampleIDRef = sample.ID

		if matchedPair := sample.GetMatchedPair(); matchedPair != nil {
			if _, exists := inputs["fastq_r1"]; !exists && matchedPair.R1Path != "" {
				if filepath.IsAbs(matchedPair.R1Path) {
					if err := validateActorFileReference(s.cfg, actor, "sample r1_path", matchedPair.R1Path); err != nil {
						return nil, err
					}
				} else {
					needsStaging = true
				}
				inputs["fastq_r1"] = matchedPair.R1Path
			}
			if _, exists := inputs["fastq_r2"]; !exists && matchedPair.R2Path != "" {
				if filepath.IsAbs(matchedPair.R2Path) {
					if err := validateActorFileReference(s.cfg, actor, "sample r2_path", matchedPair.R2Path); err != nil {
						return nil, err
					}
				} else {
					needsStaging = true
				}
				inputs["fastq_r2"] = matchedPair.R2Path
			}
		}
	}

	if req.PipelineID != "" {
		pipeline, err := s.pipelineRepo.FindByStringID(req.PipelineID)
		if err != nil {
			return nil, fmt.Errorf("pipeline not found: %s", req.PipelineID)
		}
		if !actorCanUseOwnedResource(actor, pipeline.CreatedBy) {
			return nil, fmt.Errorf("pipeline not found: %s", req.PipelineID)
		}
		if pipeline.BEDFile != "" {
			if _, exists := inputs["bed_file"]; !exists {
				if err := validateActorFileReference(s.cfg, actor, "pipeline bed_file", pipeline.BEDFile); err != nil {
					return nil, err
				}
				inputs["bed_file"] = pipeline.BEDFile
			}
		}
		if pipeline.ReferenceGenome != "" {
			if _, exists := inputs["reference_genome"]; !exists {
				inputs["reference_genome"] = pipeline.ReferenceGenome
			}
		}
		if pipeline.CNVBaseline != "" {
			if _, exists := inputs["cnv_baseline"]; !exists {
				if err := validateActorFileReference(s.cfg, actor, "pipeline cnv_baseline", pipeline.CNVBaseline); err != nil {
					return nil, err
				}
				inputs["cnv_baseline"] = pipeline.CNVBaseline
			}
		}
	}

	if req.UploadJobID != "" {
		var err error
		uploadJob, err = s.uploadJobRepo.FindByUUID(req.UploadJobID)
		if err != nil {
			return nil, fmt.Errorf("upload job not found: %s", req.UploadJobID)
		}
		if actor.Role != string(model.SystemRoleSuperAdmin) && ((actor.OrgID != "" && uploadJob.ExternalOrgID != actor.OrgID) || (actor.OrgID == "" && uploadJob.UserID != actor.UserID)) {
			return nil, fmt.Errorf("upload job not found: %s", req.UploadJobID)
		}
		files, _ := s.uploadFileRepo.FindByJobID(uploadJob.ID)
		if uploadJob.Provider == model.UploadProviderS3 {
			needsStaging = true
		}
		for _, f := range files {
			switch f.ReadType {
			case model.ReadTypeRead1:
				if _, exists := inputs["fastq_r1"]; !exists {
					inputs["fastq_r1"] = f.StorageKey
				}
			case model.ReadTypeRead2:
				if _, exists := inputs["fastq_r2"]; !exists {
					inputs["fastq_r2"] = f.StorageKey
				}
			case model.ReadTypeSingle:
				if _, exists := inputs["fastq_r1"]; !exists {
					inputs["fastq_r1"] = f.StorageKey
				}
			case model.ReadTypeBed:
				if _, exists := inputs["bed_file"]; !exists {
					inputs["bed_file"] = f.StorageKey
				}
			}
		}
	}

	inputJSON, err := json.Marshal(inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inputs: %w", err)
	}
	inputJSONStr := string(inputJSON)

	outputDir := filepath.Join(s.cfg.Task.OutputDir, workflowUUID)
	if err := ensurePathInsideBase(s.cfg.Task.OutputDir, outputDir); err != nil {
		return nil, err
	}

	configFile := s.getConfigFile(executor, "")

	taskStatus := model.TaskStatusQueued
	if needsStaging || (uploadJob != nil && uploadJob.Status != model.UploadJobStatusCompleted) {
		taskStatus = model.TaskStatusWaitingData
	}

	now := time.Now()
	task := &model.Task{
		ID:                 taskID,
		UUID:               workflowUUID,
		Name:               req.PipelineName,
		SampleID:           req.SampleID,
		InternalID:         req.InternalID,
		UploadJobID:        req.UploadJobID,
		Pipeline:           req.PipelineName,
		PipelineVersion:    req.PipelineVersion,
		Template:           req.Template,
		Executor:           executor,
		InputJSON:          inputJSONStr,
		ConfigFile:         configFile,
		OutputDir:          outputDir,
		Status:             taskStatus,
		Progress:           0,
		SampleIDRef:        sampleIDRef,
		Remark:             req.Remark,
		CreatedBy:          actor.UserID,
		ResultImportStatus: model.ResultImportStatusPending,
		ExternalOrgID:      actor.OrgID,
		EstimatedMinutes:   s.estimateTaskMinutes(sampleIDRef, uploadJob),
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.admitTask(ctx, model.OverlayAdmissionActionCreate, actor, task); err != nil {
		return nil, err
	}

	if err := s.repo.Create(task); err != nil {
		return nil, fmt.Errorf("failed to save task: %w", err)
	}

	s.emitTaskEvent(model.OverlayTaskEventCreated, actor, task, "", "")

	return task, nil
}

// StartTask starts a queued, waiting_for_data, or failed task.
// Before launching, it verifies that all input data files are accessible.
func (s *TaskService) StartTask(ctx context.Context, id string, actor model.OverlayActor) (*model.Task, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusQueued &&
		task.Status != model.TaskStatusFailed &&
		task.Status != model.TaskStatusWaitingData {
		return nil, fmt.Errorf("task cannot be started from status: %s", task.Status)
	}

	if task.Status == model.TaskStatusWaitingData {
		ready, reason := s.checkDataReady(task)
		if !ready {
			return nil, fmt.Errorf("data not ready: %s", reason)
		}
	}

	if err := s.stageDataFiles(task); err != nil {
		return nil, fmt.Errorf("failed to stage data files: %w", err)
	}

	previousStatus := task.Status
	if err := s.admitTask(ctx, model.OverlayAdmissionActionStart, actor, task); err != nil {
		return nil, err
	}

	task.Status = model.TaskStatusRunning
	task.Progress = 0
	task.Error = ""
	now := time.Now()
	task.StartedAt = &now
	task.UpdatedAt = now

	if err := s.repo.Update(task); err != nil {
		s.emitTaskEvent(model.OverlayTaskEventStartFailed, actor, task, previousStatus, err.Error())
		return nil, err
	}

	s.emitTaskEvent(model.OverlayTaskEventRunning, actor, task, previousStatus, "")
	go s.launchTask(task)

	return task, nil
}

// StopTask stops a running task
func (s *TaskService) StopTask(ctx context.Context, id string, actor model.OverlayActor) (*model.Task, error) {
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

	s.emitTaskEvent(model.OverlayTaskEventQueued, actor, task, model.TaskStatusRunning, "")
	return task, nil
}

// RetryTask retries a failed, cancelled, or waiting_for_data task
func (s *TaskService) RetryTask(ctx context.Context, id string, actor model.OverlayActor) (*model.Task, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusFailed &&
		task.Status != model.TaskStatusCancelled &&
		task.Status != model.TaskStatusWaitingData {
		return nil, fmt.Errorf("task cannot be retried from status: %s", task.Status)
	}

	if task.Status == model.TaskStatusWaitingData {
		ready, reason := s.checkDataReady(task)
		if !ready {
			return nil, fmt.Errorf("data still not ready: %s", reason)
		}
	}

	if err := s.stageDataFiles(task); err != nil {
		return nil, fmt.Errorf("failed to stage data files: %w", err)
	}

	previousStatus := task.Status
	if err := s.admitTask(ctx, model.OverlayAdmissionActionRetry, actor, task); err != nil {
		return nil, err
	}

	task.Status = model.TaskStatusRunning
	task.Progress = 0
	task.Error = ""
	now := time.Now()
	task.StartedAt = &now
	task.UpdatedAt = now

	if err := s.repo.Update(task); err != nil {
		s.emitTaskEvent(model.OverlayTaskEventStartFailed, actor, task, previousStatus, err.Error())
		return nil, err
	}

	s.emitTaskEvent(model.OverlayTaskEventRunning, actor, task, previousStatus, "")
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

		previousStatus := task.Status
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
		s.emitStatusEvent(task, previousStatus)
		if task.Status == model.TaskStatusCompleted {
			s.importTaskArchive(task)
		}
	}()
}

func (s *TaskService) updateTaskError(task *model.Task, errMsg string) {
	s.mu.Lock()
	previousStatus := task.Status
	task.Status = model.TaskStatusFailed
	task.Error = errMsg
	now := time.Now()
	task.FinishedAt = &now
	task.UpdatedAt = now
	s.repo.Update(task)
	s.mu.Unlock()
	s.emitTaskEvent(model.OverlayTaskEventFailed, model.OverlayActor{}, task, previousStatus, errMsg)
}

func (s *TaskService) importTaskArchive(task *model.Task) {
	if task == nil || task.Status != model.TaskStatusCompleted {
		return
	}
	if task.ResultImportStatus == model.ResultImportStatusRunning {
		return
	}

	archiveDir := filepath.Join(s.cfg.Task.ArchiveDir, task.UUID)
	if s.cfg.Task.ArchiveDir == "" {
		return
	}
	if _, err := os.Stat(archiveDir); err != nil {
		return
	}
	fingerprint := archiveImportFingerprint(task, archiveDir)
	if task.ResultImportStatus == model.ResultImportStatusSuccess && task.ResultImportFingerprint == fingerprint {
		return
	}

	s.runTaskArchiveImport(task, archiveDir)
}

func (s *TaskService) runTaskArchiveImport(task *model.Task, archiveDir string) {
	fingerprint := archiveImportFingerprint(task, archiveDir)
	now := time.Now()
	task.ResultImportStatus = model.ResultImportStatusRunning
	task.ResultImportError = ""
	task.ResultImportedAt = nil
	task.ResultImportAttempts++
	task.UpdatedAt = now
	_ = s.repo.Update(task)

	batch := s.startResultImportBatch(task, archiveDir, fingerprint, now)
	result, err := NewImporter(s.cfg).ImportFromTaskArchive(task, archiveDir)
	finishedAt := time.Now()
	task.ResultImportedAt = &finishedAt
	task.UpdatedAt = finishedAt
	if err != nil {
		task.ResultImportStatus = model.ResultImportStatusFailed
		task.ResultImportError = err.Error()
	} else if result != nil && !result.Success {
		task.ResultImportStatus = model.ResultImportStatusFailed
		task.ResultImportError = result.Error
	} else {
		task.ResultImportStatus = model.ResultImportStatusSuccess
		task.ResultImportError = ""
		task.ResultImportFingerprint = fingerprint
	}
	_ = s.repo.Update(task)
	s.finishResultImportBatch(batch, result, task.ResultImportStatus, task.ResultImportError, finishedAt)
}

func (s *TaskService) startResultImportBatch(task *model.Task, archiveDir, fingerprint string, startedAt time.Time) *model.ResultImportBatch {
	if task == nil || s.importBatchRepo == nil {
		return nil
	}

	batch := &model.ResultImportBatch{
		TaskUUID:      task.UUID,
		Source:        "local",
		Status:        model.ResultImportBatchStatusRunning,
		Fingerprint:   fingerprint,
		ArchiveBase:   s.cfg.Task.ArchiveDir,
		ArchivePrefix: task.UUID,
		OutputsKey:    "outputs.resolved.json",
		StartedAt:     startedAt,
	}
	if err := s.importBatchRepo.Create(batch); err != nil {
		fmt.Printf("WARNING: failed to create result import batch for task %s: %v\n", task.UUID, err)
		return nil
	}
	return batch
}

func (s *TaskService) finishResultImportBatch(batch *model.ResultImportBatch, result *ImportResult, status model.ResultImportStatus, importErr string, finishedAt time.Time) {
	if batch == nil || s.importBatchRepo == nil {
		return
	}

	batch.FinishedAt = &finishedAt
	batch.Error = importErr
	switch status {
	case model.ResultImportStatusSuccess:
		batch.Status = model.ResultImportBatchStatusSuccess
	default:
		batch.Status = model.ResultImportBatchStatusFailed
	}
	if result != nil {
		batch.ObjectKeysJSON = marshalImportAuditJSON(result.SourceFiles)
		batch.CountsJSON = marshalImportAuditJSON(result.Counts)
		if batch.Error == "" {
			batch.Error = result.Error
		}
	}
	if err := s.importBatchRepo.Update(batch); err != nil {
		fmt.Printf("WARNING: failed to update result import batch for task %s: %v\n", batch.TaskUUID, err)
	}
}

func marshalImportAuditJSON(value interface{}) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func archiveImportFingerprint(task *model.Task, archiveDir string) string {
	if task == nil {
		return ""
	}
	outputsPath := filepath.Join(archiveDir, "outputs.resolved.json")
	var outputsMod int64
	if info, err := os.Stat(outputsPath); err == nil {
		outputsMod = info.ModTime().UnixNano()
	}
	payload := fmt.Sprintf("%s|%s|%d", task.UUID, archiveDir, outputsMod)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", sum[:])
}

// RetryResultImport resets structured result import state and runs the archive import again.
func (s *TaskService) RetryResultImport(ctx context.Context, id string) (*model.TaskProgressResponse, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	if task.Status != model.TaskStatusCompleted {
		return nil, fmt.Errorf("task is not completed")
	}
	if task.ResultImportStatus == model.ResultImportStatusRunning {
		return nil, fmt.Errorf("result import is already running")
	}

	archiveDir := filepath.Join(s.cfg.Task.ArchiveDir, task.UUID)
	if s.cfg.Task.ArchiveDir == "" {
		return nil, fmt.Errorf("archive directory is not configured")
	}
	if _, err := os.Stat(archiveDir); err != nil {
		return nil, fmt.Errorf("archive not found for task: %s", id)
	}

	task.ResultImportStatus = model.ResultImportStatusPending
	task.ResultImportError = ""
	task.ResultImportedAt = nil
	task.ResultImportFingerprint = ""
	task.UpdatedAt = time.Now()
	if err := s.repo.Update(task); err != nil {
		return nil, err
	}

	s.runTaskArchiveImport(task, archiveDir)
	return s.GetTaskProgress(ctx, id)
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
		ID:                      task.UUID,
		UUID:                    task.UUID,
		Name:                    task.Name,
		Template:                task.Template,
		Status:                  task.Status,
		Progress:                task.Progress,
		CreatedAt:               task.CreatedAt,
		ResultImportStatus:      task.ResultImportStatus,
		ResultImportError:       task.ResultImportError,
		ResultImportedAt:        task.ResultImportedAt,
		ResultImportFingerprint: task.ResultImportFingerprint,
		ResultImportAttempts:    task.ResultImportAttempts,
	}

	// Query Sepiida for real-time progress
	if s.sepiida != nil && task.UUID != "" {
		workflow, tasks, err := s.sepiida.GetWorkflowWithTasks(task.UUID)
		if err == nil && workflow != nil {
			resp.Sepiida = workflow
			resp.Tasks = tasks
			s.syncTaskFromSepiida(task)
			resp.Status = task.Status
			resp.Progress = task.Progress
			resp.ResultImportStatus = task.ResultImportStatus
			resp.ResultImportError = task.ResultImportError
			resp.ResultImportedAt = task.ResultImportedAt
			resp.ResultImportFingerprint = task.ResultImportFingerprint
			resp.ResultImportAttempts = task.ResultImportAttempts
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

// ListTasksAudit lists tasks with the enriched audit response shape (for
// cross-org monitoring consumers like Cuttlefish). Reuses PaginateByQuery,
// which now honors Search/CreatedSince/UpdatedSince filters.
func (s *TaskService) ListTasksAudit(ctx context.Context, query *model.TaskListQuery) (*model.TaskAuditListResponse, error) {
	tasks, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.TaskAuditResponse, len(tasks))
	for i, task := range tasks {
		items[i] = task.ToAuditResponse()
	}

	return &model.TaskAuditListResponse{
		Total: total,
		Items: items,
	}, nil
}

// CancelTask cancels a running task
func (s *TaskService) CancelTask(ctx context.Context, id string, actor model.OverlayActor) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusRunning && task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusWaitingData {
		return fmt.Errorf("task is not running or queued")
	}

	if cmd, ok := s.running[id]; ok {
		cmd.Process.Kill()
	}

	task.Status = model.TaskStatusCancelled
	now := time.Now()
	task.FinishedAt = &now
	task.UpdatedAt = now

	if err := s.repo.Update(task); err != nil {
		return err
	}
	s.emitTaskEvent(model.OverlayTaskEventCancelled, actor, task, "", "")
	return nil
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

	taskOutputDir := filepath.Join(s.cfg.Task.OutputDir, task.UUID)
	if task.LogPath != "" {
		if logs, err := readTaskLogFile(taskOutputDir, task.LogPath); err == nil {
			return logs, nil
		}
	}

	if task.UUID != "" {
		lastLink := filepath.Join(s.cfg.Task.OutputDir, task.UUID, "_LAST")
		if target, err := os.Readlink(lastLink); err == nil {
			lastDir := filepath.Join(taskOutputDir, target)
			workflowLog := filepath.Join(lastDir, "workflow.log")
			if logs, err := readTaskLogFile(taskOutputDir, workflowLog); err == nil {
				return logs, nil
			} else if strings.Contains(err.Error(), "escapes base directory") || strings.Contains(err.Error(), "not regular") {
				return "", fmt.Errorf("invalid log symlink target")
			}
		}
	}

	return "", fmt.Errorf("no log file available")
}

// StartDataWaitSync starts a background goroutine that periodically checks tasks
// in waiting_for_data status. When their upload job is completed and data files are
// accessible, the task transitions to queued automatically.
func (s *TaskService) StartDataWaitSync(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkWaitingDataTasks()
			}
		}
	}()
}

func (s *TaskService) checkWaitingDataTasks() {
	tasks, err := s.repo.FindByStatuses([]model.TaskStatus{model.TaskStatusWaitingData})
	if err != nil {
		return
	}

	for _, task := range tasks {
		ready, _ := s.checkDataReady(&task)
		if ready {
			task.Status = model.TaskStatusQueued
			task.UpdatedAt = time.Now()
			s.repo.Update(&task)
		}
	}
}

func (s *TaskService) checkDataReady(task *model.Task) (ready bool, reason string) {
	if err := s.stageDataFiles(task); err != nil {
		return false, err.Error()
	}
	var inputs map[string]interface{}
	if task.InputJSON != "" {
		if err := json.Unmarshal([]byte(task.InputJSON), &inputs); err != nil {
			return false, "failed to parse input JSON"
		}
	}
	if inputs == nil {
		inputs = make(map[string]interface{})
	}

	if task.UploadJobID != "" {
		uploadJob, err := s.uploadJobRepo.FindByUUID(task.UploadJobID)
		if err != nil {
			return false, "upload job not found"
		}
		if uploadJob.Status != model.UploadJobStatusCompleted {
			return false, fmt.Sprintf("upload job status is %s", uploadJob.Status)
		}
		files, err := s.uploadFileRepo.FindByJobID(uploadJob.ID)
		if err != nil {
			return false, "upload files not found"
		}
		for _, f := range files {
			if f.Status != model.FileStatusCompleted {
				return false, fmt.Sprintf("upload file %s status is %s", f.UUID, f.Status)
			}
			if strings.TrimSpace(f.StorageKey) == "" {
				return false, fmt.Sprintf("upload file %s has no storage path", f.UUID)
			}
		}
		if applyUploadFilesToInputs(inputs, files) {
			data, err := json.Marshal(inputs)
			if err != nil {
				return false, "failed to update input JSON"
			}
			task.InputJSON = string(data)
			task.UpdatedAt = time.Now()
			_ = s.repo.Update(task)
		}
	}

	for key, val := range inputs {
		path, ok := val.(string)
		if !ok || path == "" {
			continue
		}
		if !containsAny(key, "fastq", "file", "bed", "reference") {
			continue
		}

		if strings.HasPrefix(path, "cos://") || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			return false, fmt.Sprintf("remote file paths are not supported in local Octopus: %s", path)
		}

		if _, err := os.Stat(path); os.IsNotExist(err) {
			return false, fmt.Sprintf("file not found: %s", path)
		}
	}

	return true, ""
}

func applyUploadFilesToInputs(inputs map[string]interface{}, files []model.UploadFile) bool {
	changed := false
	set := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if current, ok := inputs[key].(string); ok {
			current = strings.TrimSpace(current)
			if current == value {
				return
			}
			if filepath.IsAbs(current) {
				if _, err := os.Stat(current); err == nil {
					return
				}
			}
		}
		inputs[key] = value
		changed = true
	}

	for _, f := range files {
		switch f.ReadType {
		case model.ReadTypeRead1:
			set("fastq_r1", f.StorageKey)
		case model.ReadTypeRead2:
			set("fastq_r2", f.StorageKey)
		case model.ReadTypeSingle:
			set("fastq_r1", f.StorageKey)
		case model.ReadTypeBed:
			set("bed_file", f.StorageKey)
		}
	}
	return changed
}

func (s *TaskService) stageDataFiles(task *model.Task) error {
	if task == nil {
		return fmt.Errorf("task is required")
	}
	assets := make(map[uint]*model.DataAsset)
	if task.SampleIDRef != 0 {
		var link model.SampleDataLink
		if err := database.GetDB().Where("sample_id = ?", task.SampleIDRef).First(&link).Error; err == nil {
			for _, id := range []uint{link.Read1AssetID, link.Read2AssetID} {
				if asset, err := s.assetRepo.FindByID(id); err == nil {
					assets[id] = asset
				}
			}
		}
	}
	if task.UploadJobID != "" {
		if job, err := s.uploadJobRepo.FindByUUID(task.UploadJobID); err == nil {
			if files, err := s.uploadFileRepo.FindByJobID(job.ID); err == nil {
				for i := range files {
					if asset, err := s.assetRepo.FindByUploadFileID(files[i].ID); err == nil {
						assets[asset.ID] = asset
					}
				}
			}
		}
	}
	var remote []*model.DataAsset
	for _, asset := range assets {
		if asset.Provider == model.UploadProviderS3 {
			if asset.Status != model.FileStatusCompleted {
				return fmt.Errorf("data asset %s is not ready", asset.UUID)
			}
			remote = append(remote, asset)
		}
	}
	if len(remote) == 0 {
		return nil
	}

	inputDir := filepath.Join(task.OutputDir, "_inputs")
	if err := ensurePathInsideBase(s.cfg.Task.OutputDir, inputDir); err != nil {
		return err
	}
	if err := os.MkdirAll(inputDir, 0750); err != nil {
		return err
	}
	storage, err := newS3Storage(context.Background(), s.cfg.Storage)
	if err != nil {
		return err
	}
	var inputs map[string]interface{}
	if task.InputJSON != "" {
		_ = json.Unmarshal([]byte(task.InputJSON), &inputs)
	}
	if inputs == nil {
		inputs = make(map[string]interface{})
	}
	for _, asset := range remote {
		name := filepath.Base(strings.ReplaceAll(asset.FileName, `\`, "/"))
		destination := filepath.Join(inputDir, asset.UUID+"-"+name)
		if err := ensurePathInsideBase(inputDir, destination); err != nil {
			return err
		}
		if info, err := os.Stat(destination); err != nil || !info.Mode().IsRegular() || info.Size() != asset.FileSize {
			temporary := destination + ".part"
			_ = os.Remove(temporary)
			source, err := storage.open(context.Background(), asset.StorageKey)
			if err != nil {
				return fmt.Errorf("download data asset %s: %w", asset.UUID, err)
			}
			file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				source.Close()
				return err
			}
			written, copyErr := io.Copy(file, source)
			closeErr := file.Close()
			source.Close()
			if copyErr != nil || closeErr != nil || written != asset.FileSize {
				_ = os.Remove(temporary)
				if copyErr != nil {
					return copyErr
				}
				if closeErr != nil {
					return closeErr
				}
				return fmt.Errorf("downloaded size mismatch for data asset %s", asset.UUID)
			}
			if err := os.Rename(temporary, destination); err != nil {
				_ = os.Remove(temporary)
				return err
			}
		}
		switch asset.ReadType {
		case model.ReadTypeRead1:
			inputs["fastq_r1"] = destination
		case model.ReadTypeRead2:
			inputs["fastq_r2"] = destination
		case model.ReadTypeSingle:
			inputs["fastq_r1"] = destination
		case model.ReadTypeBed:
			inputs["bed_file"] = destination
		}
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	task.InputJSON = string(encoded)
	task.UpdatedAt = time.Now()
	return s.repo.Update(task)
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		for i := 0; i < len(s); i++ {
			end := i + len(sub)
			if end > len(s) {
				break
			}
			match := true
			for j := 0; j < len(sub); j++ {
				c1 := s[i+j]
				c2 := sub[j]
				if c1 >= 'A' && c1 <= 'Z' {
					c1 += 32
				}
				if c2 >= 'A' && c2 <= 'Z' {
					c2 += 32
				}
				if c1 != c2 {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}
