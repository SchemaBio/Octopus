package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/sepiida"
	"github.com/SchemaBio/Octopus/internal/workflow"
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
	baselineRepo    *repository.CNVBaselineRepository
	pedigreeRepo    *repository.PedigreeRepository
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
		baselineRepo:    repository.NewCNVBaselineRepository(),
		pedigreeRepo:    repository.NewPedigreeRepository(),
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
	if err := s.emitTaskEventWithError(event, actor, task, previousStatus, message); err != nil {
		fmt.Printf("WARNING: overlay task event %s failed for %s: %v\n", event, task.UUID, err)
	}
}

func (s *TaskService) emitTaskEventWithError(event string, actor model.OverlayActor, task *model.Task, previousStatus model.TaskStatus, message string) error {
	if s.overlay == nil || task == nil {
		return nil
	}
	req := model.OverlayTaskEventRequest{
		Event:          event,
		Actor:          actor,
		Task:           model.NewOverlayTaskSnapshot(task),
		PreviousStatus: previousStatus,
		OccurredAt:     time.Now(),
		Message:        message,
	}
	return s.overlay.EmitTaskEvent(context.Background(), req)
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

	pending, err := s.repo.FindPendingCVMArchiveTermination()
	if err != nil {
		return
	}
	for i := range pending {
		s.notifyCVMArchiveCompletion(&pending[i])
	}
}

func (s *TaskService) syncTaskFromSepiida(task *model.Task) {
	if s.sepiida == nil || task.UUID == "" {
		return
	}

	workflow, _, err := s.sepiida.GetWorkflowWithTasks(sepiidaWorkflowUUID(task))
	if err != nil || workflow == nil {
		return
	}

	completedNow := false
	previousStatus := task.Status
	archiveMetadata := workflow.NormalizedArchiveMetadata()
	if task.Executor == model.ExecutorCVM && workflow.Status == model.SepiidaStatusSuccess {
		if !archiveMetadata.Archived {
			if task.Progress < 99 {
				task.Progress = 99
				task.UpdatedAt = time.Now()
				_ = s.repo.Update(task)
			}
			return
		}
		if err := s.stageCVMArchive(task, archiveMetadata); err != nil {
			fmt.Printf("WARNING: stage COS archive for task %s: %v\n", task.UUID, err)
			return
		}
	}

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
		if err := s.repo.Update(task); err != nil {
			s.mu.Unlock()
			fmt.Printf("WARNING: persist Sepiida task status for %s: %v\n", task.UUID, err)
			return
		}
		s.syncCNVBaselineOutput(task, workflow.OutputsJSON)
		s.emitStatusEvent(task, previousStatus)
	}
	s.mu.Unlock()

	if completedNow {
		s.importTaskArchive(task)
	}
	if task.Executor == model.ExecutorCVM {
		s.notifyCVMArchiveCompletion(task)
	}
}

func (s *TaskService) stageCVMArchive(task *model.Task, metadata model.SepiidaArchiveMetadata) error {
	if task == nil || task.Executor != model.ExecutorCVM {
		return fmt.Errorf("CVM task is required")
	}
	if task.CVMArchiveStagedAt != nil {
		return nil
	}
	if err := s.stageCOSArchive(task, metadata); err != nil {
		return err
	}
	now := time.Now()
	task.CVMArchiveStagedAt = &now
	task.UpdatedAt = now
	return s.repo.Update(task)
}

func (s *TaskService) notifyCVMArchiveCompletion(task *model.Task) {
	if !cvmArchiveTerminationPending(task) {
		return
	}
	if err := s.emitTaskEventWithError(model.OverlayTaskEventArchiveCompleted, model.OverlayActor{}, task, model.TaskStatusCompleted, "COS archive staged"); err != nil {
		fmt.Printf("WARNING: notify CVM archive completion for task %s: %v\n", task.UUID, err)
		return
	}
	now := time.Now()
	task.CVMArchiveTerminationNotifiedAt = &now
	task.UpdatedAt = now
	if err := s.repo.Update(task); err != nil {
		// The event is idempotent for its attempt. Leave the timestamp unset so
		// the next sync retransmits it rather than leaking the spot instance.
		task.CVMArchiveTerminationNotifiedAt = nil
		fmt.Printf("WARNING: persist CVM archive completion for task %s: %v\n", task.UUID, err)
	}
}

func cvmArchiveTerminationPending(task *model.Task) bool {
	return task != nil && task.Executor == model.ExecutorCVM && task.CVMArchiveStagedAt != nil && task.CVMArchiveTerminationNotifiedAt == nil && task.ExecutionAttemptID != ""
}

func sepiidaWorkflowUUID(task *model.Task) string {
	if task != nil && task.Executor == model.ExecutorCVM && task.ExecutionAttemptID != "" {
		return task.ExecutionAttemptID
	}
	if task == nil {
		return ""
	}
	return task.UUID
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

func (s *TaskService) resolveAnalysisPipeline(req *model.TaskCreateRequest, actor model.OverlayActor) (*model.Pipeline, error) {
	setGenome := func(genome string) {
		if req.Inputs == nil {
			req.Inputs = make(map[string]interface{})
		}
		req.Inputs["reference_genome"] = genome
	}
	switch req.PipelineID {
	case model.BuiltinPipelineWESSingleID:
		req.PipelineName, req.PipelineVersion, req.Template = "WES单样本分析", "builtin-v1", "single"
		setGenome("hg19")
		return nil, nil
	case model.BuiltinPipelineWESFamilyID:
		req.PipelineName, req.PipelineVersion, req.Template = "WES家系分析", "builtin-v1", "trio"
		setGenome("hg19")
		return nil, nil
	case model.BuiltinPipelineWESSingleHG38ID:
		req.PipelineName, req.PipelineVersion, req.Template = "WES单样本分析（hg38）", "builtin-v1", "single"
		setGenome("hg38")
		return nil, nil
	case model.BuiltinPipelineWESFamilyHG38ID:
		req.PipelineName, req.PipelineVersion, req.Template = "WES家系分析（hg38）", "builtin-v1", "trio"
		setGenome("hg38")
		return nil, nil
	case "":
		return nil, fmt.Errorf("pipelineId is required")
	}

	pipeline, err := s.pipelineRepo.FindScopedByUUID(req.PipelineID, actor)
	if err != nil || pipeline.Status != model.PipelineStatusActive || !isAllowedPipelineBase(pipeline.BaseType) {
		return nil, fmt.Errorf("pipeline not found: %s", req.PipelineID)
	}
	req.PipelineName = pipeline.Name
	req.PipelineVersion = pipeline.Version
	req.Template = model.PipelineTemplate(pipeline.BaseType)
	return pipeline, nil
}

// CreateTask creates a new task. It resolves data file paths from the linked Sample,
// UploadJob, and Pipeline configuration, injecting them into WDL inputs.
// If the upload job is not yet completed, the task enters waiting_for_data status.
func (s *TaskService) CreateTask(ctx context.Context, req *model.TaskCreateRequest, actor model.OverlayActor) (*model.Task, error) {
	executor := model.ExecutorType(s.cfg.Task.DefaultExecutor)
	switch executor {
	case model.ExecutorLocal, model.ExecutorSlurm, model.ExecutorLSF, model.ExecutorCVM:
	default:
		executor = model.ExecutorLocal
	}
	var selectedPipeline *model.Pipeline
	var err error
	// InputAssets is server-only and is used by the CNV baseline workflow. All
	// browser-created analysis tasks must resolve one allowed pipeline ID here.
	if len(req.InputAssets) == 0 {
		selectedPipeline, err = s.resolveAnalysisPipeline(req, actor)
		if err != nil {
			return nil, err
		}
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
	if req.Template == "baseline_fix" {
		inputs["CNVBaselineFix.prefix"] = taskOutputPrefix(workflowUUID)
	}
	if err := validateActorTaskFileInputs(s.cfg, actor, inputs); err != nil {
		return nil, err
	}

	directAssets := make([]model.TaskDataAsset, 0, len(req.InputAssets))
	read1Inputs := make([]string, 0)
	read2Inputs := make([]string, 0)
	for _, inputAsset := range req.InputAssets {
		asset, err := s.assetRepo.FindByID(inputAsset.AssetID)
		if err != nil || asset.Status != model.FileStatusCompleted ||
			(actor.Role != string(model.SystemRoleSuperAdmin) && ((actor.OrgID != "" && asset.ExternalOrgID != actor.OrgID) || (actor.OrgID == "" && (asset.ExternalOrgID != "" || asset.CreatedBy != actor.UserID)))) {
			return nil, fmt.Errorf("data asset not found")
		}
		directAssets = append(directAssets, model.TaskDataAsset{AssetID: asset.ID, InputRole: inputAsset.InputRole, InputIndex: inputAsset.Index})
		switch inputAsset.InputRole {
		case model.TaskAssetRoleCNVRead1:
			read1Inputs = setIndexedInput(read1Inputs, inputAsset.Index, asset.StorageKey)
		case model.TaskAssetRoleCNVRead2:
			read2Inputs = setIndexedInput(read2Inputs, inputAsset.Index, asset.StorageKey)
		case model.TaskAssetRoleCNVBED:
			inputs["CNVBaselineFix.bed"] = asset.StorageKey
		default:
			return nil, fmt.Errorf("unsupported task data asset role: %s", inputAsset.InputRole)
		}
	}
	if len(read1Inputs) > 0 {
		inputs["CNVBaselineFix.read_1"] = read1Inputs
	}
	if len(read2Inputs) > 0 {
		inputs["CNVBaselineFix.read_2"] = read2Inputs
	}

	var sampleIDRef uint
	sampleDataReady := false
	var uploadJob *model.UploadJob
	needsStaging := false

	if req.Template == "trio" {
		if strings.TrimSpace(req.PedigreeID) == "" {
			return nil, fmt.Errorf("pedigreeId is required for trio analysis")
		}
		proband, trioAssets, err := s.prepareTrioInputs(req.PedigreeID, workflowUUID, actor, inputs)
		if err != nil {
			return nil, err
		}
		req.SampleID, req.InternalID = proband.UUID, proband.InternalID
		sampleIDRef, sampleDataReady = proband.ID, true
		directAssets = append(directAssets, trioAssets...)
	} else if req.SampleID != "" {
		sample, err := s.sampleRepo.FindScopedByUUID(req.SampleID, actor)
		if err != nil {
			return nil, fmt.Errorf("sample not found: %s", req.SampleID)
		}
		sampleIDRef = sample.ID

		if matchedPair := sample.GetMatchedPair(); matchedPairComplete(matchedPair) {
			sampleDataReady = true
			if _, exists := inputs["fastq_r1"]; !exists && matchedPair.R1Path != "" {
				if filepath.IsAbs(matchedPair.R1Path) {
					if err := validateActorFileReference(s.cfg, actor, "sample r1_path", matchedPair.R1Path); err != nil {
						return nil, err
					}
				} else if executor != model.ExecutorCVM {
					needsStaging = true
				}
				inputs["fastq_r1"] = matchedPair.R1Path
			}
			if _, exists := inputs["fastq_r2"]; !exists && matchedPair.R2Path != "" {
				if filepath.IsAbs(matchedPair.R2Path) {
					if err := validateActorFileReference(s.cfg, actor, "sample r2_path", matchedPair.R2Path); err != nil {
						return nil, err
					}
				} else if executor != model.ExecutorCVM {
					needsStaging = true
				}
				inputs["fastq_r2"] = matchedPair.R2Path
			}
		}
	}
	if req.Template != "trio" && req.SampleID == "" && len(req.InputAssets) == 0 {
		return nil, fmt.Errorf("sampleId is required for single-sample analysis")
	}

	if selectedPipeline != nil {
		if selectedPipeline.BEDAssetID != nil {
			bed, err := s.assetRepo.FindByID(*selectedPipeline.BEDAssetID)
			if err != nil || bed.Status != model.FileStatusCompleted || bed.ReadType != model.ReadTypeBed {
				return nil, fmt.Errorf("pipeline BED data asset is not available")
			}
			if _, exists := inputs["bed_file"]; !exists {
				inputs["bed_file"] = bed.StorageKey
			}
			directAssets = append(directAssets, model.TaskDataAsset{AssetID: bed.ID, InputRole: model.TaskAssetRoleAnalysisBED, InputIndex: 0})
		}
		if selectedPipeline.ReferenceGenome != "" {
			if _, exists := inputs["reference_genome"]; !exists {
				inputs["reference_genome"] = selectedPipeline.ReferenceGenome
			}
		}
		if selectedPipeline.CNVBaselineID != nil {
			baseline, err := s.baselineRepo.FindByID(*selectedPipeline.CNVBaselineID)
			if err != nil || strings.TrimSpace(baseline.OutputPath) == "" {
				return nil, fmt.Errorf("pipeline CNV baseline is not available")
			}
			if _, exists := inputs["cnv_baseline"]; !exists {
				inputs["cnv_baseline"] = baseline.OutputPath
			}
		}
	}

	if req.UploadJobID != "" {
		var err error
		uploadJob, err = s.uploadJobRepo.FindByUUID(req.UploadJobID)
		if err != nil {
			return nil, fmt.Errorf("upload job not found: %s", req.UploadJobID)
		}
		if !uploadJobUseAllowed(uploadJob, actor) {
			return nil, fmt.Errorf("upload job not found: %s", req.UploadJobID)
		}
		files, _ := s.uploadFileRepo.FindByJobID(uploadJob.ID)
		if uploadJob.Provider == model.UploadProviderS3 && executor != model.ExecutorCVM {
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

	if req.Template == "single" || req.Template == "trio" {
		inputs, err = buildCVMWDLInputs(req.Template, fmt.Sprint(inputs["reference_genome"]), inputs)
		if err != nil {
			return nil, err
		}
		prefix := "SingleWES"
		if req.Template == "trio" {
			prefix = "TrioWES"
		}
		inputs[prefix+".prefix"] = taskOutputPrefix(workflowUUID)
		thresholds, configErr := NewWorkflowConfigService().Get(req.Template, fmt.Sprint(inputs["reference_genome"]))
		if configErr != nil {
			return nil, configErr
		}
		applyWorkflowThresholds(inputs, thresholds)
		if req.Template == "trio" {
			inputs["TrioWES.ped"] = filepath.Join(s.cfg.Task.OutputDir, workflowUUID, taskOutputPrefix(workflowUUID)+".ped")
		}
	} else if req.Template == "baseline_fix" {
		inputs, err = buildCVMWDLInputs(req.Template, fmt.Sprint(inputs["reference_genome"]), inputs)
		if err != nil {
			return nil, err
		}
		inputs["CNVBaselineFix.prefix"] = taskOutputPrefix(workflowUUID)
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
	if (req.SampleID != "" && !sampleDataReady) || needsStaging || (uploadJob != nil && uploadJob.Status != model.UploadJobStatusCompleted) {
		taskStatus = model.TaskStatusWaitingData
	}

	now := time.Now()
	task := &model.Task{
		ID:                 taskID,
		UUID:               workflowUUID,
		Name:               req.PipelineName,
		SampleID:           req.SampleID,
		PedigreeID:         req.PedigreeID,
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
		TenantID:           model.TenantIDForIdentity(actor.OrgID, actor.UserID),
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
	for i := range directAssets {
		directAssets[i].TaskUUID = task.UUID
	}
	if len(directAssets) > 0 {
		if err := database.GetDB().Create(&directAssets).Error; err != nil {
			_ = s.repo.DeleteByID(task.ID)
			return nil, fmt.Errorf("failed to link task data assets: %w", err)
		}
	}

	s.emitTaskEvent(model.OverlayTaskEventCreated, actor, task, "", "")
	if task.Status == model.TaskStatusQueued && !req.DeferStart {
		if started, startErr := s.StartTask(ctx, task.UUID, actor); startErr == nil {
			return started, nil
		}
	}

	return task, nil
}

func (s *TaskService) DiscardDraftTask(task *model.Task) {
	if task == nil {
		return
	}
	_ = database.GetDB().Where("task_uuid = ?", task.UUID).Delete(&model.TaskDataAsset{}).Error
	_ = s.repo.DeleteByID(task.ID)
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

	if task.Executor != model.ExecutorCVM {
		if err := s.stageDataFiles(task); err != nil {
			return nil, fmt.Errorf("failed to stage data files: %w", err)
		}
	}

	previousStatus := task.Status
	if task.Executor == model.ExecutorCVM {
		if err := s.prepareCVMExecutionAttempt(task, false); err != nil {
			return nil, err
		}
	} else if task.ExecutionAttemptID == "" || previousStatus == model.TaskStatusFailed || previousStatus == model.TaskStatusCancelled {
		// Local/Slurm/LSF tasks also need a stable attempt identity so a
		// re-import of the same attempt cannot double-count a review event.
		task.ExecutionAttemptID = uuid.New().String()
		task.UpdatedAt = time.Now()
		if err := s.repo.Update(task); err != nil {
			return nil, fmt.Errorf("persist execution attempt: %w", err)
		}
	}
	if err := s.admitTask(ctx, model.OverlayAdmissionActionStart, actor, task); err != nil {
		return nil, err
	}

	var dispatch *model.CVMDispatchResponse
	if task.Executor == model.ExecutorCVM {
		request, err := s.buildCVMDispatchRequest(ctx, actor, task)
		if err != nil {
			s.markCVMStartFailed(task)
			s.emitTaskEvent(model.OverlayTaskEventStartFailed, actor, task, previousStatus, err.Error())
			return nil, err
		}
		dispatch, err = s.overlay.DispatchCVMTask(ctx, request)
		if err != nil {
			// The request may have reached Squid or Tencent Cloud. Preserve the
			// attempt and its reservation so a repeated Start is idempotent.
			return nil, err
		}
	}

	task.Status = model.TaskStatusRunning
	task.Progress = 0
	task.Error = ""
	now := time.Now()
	task.StartedAt = &now
	task.UpdatedAt = now
	if dispatch != nil {
		task.CVMInstanceID = dispatch.InstanceID
		task.VMStatus = dispatch.InstanceState
	}

	if err := s.repo.Update(task); err != nil {
		if dispatch != nil {
			_ = s.overlay.CancelCVMTask(context.Background(), model.CVMCancelRequest{Actor: actor, TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, Reason: "Octopus task update failed"})
			s.markCVMStartFailed(task)
		}
		s.emitTaskEvent(model.OverlayTaskEventStartFailed, actor, task, previousStatus, err.Error())
		return nil, err
	}

	s.emitTaskEvent(model.OverlayTaskEventRunning, actor, task, previousStatus, "")
	if task.Executor != model.ExecutorCVM {
		go s.launchTask(task)
	}

	return task, nil
}

// StopTask stops a running task
func (s *TaskService) StopTask(ctx context.Context, id string, actor model.OverlayActor) (*model.Task, error) {
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusRunning {
		return nil, fmt.Errorf("task is not running")
	}
	if task.Executor == model.ExecutorCVM {
		if err := s.overlay.CancelCVMTask(ctx, model.CVMCancelRequest{Actor: actor, TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, Reason: "task stopped by user"}); err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	if cmd, ok := s.running[id]; ok {
		_ = cmd.Process.Kill()
		delete(s.running, id)
	}
	s.mu.Unlock()

	task.Status = model.TaskStatusQueued
	task.Progress = 0
	if task.Executor == model.ExecutorCVM {
		task.VMStatus = "TERMINATED"
	}
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

	if task.Executor != model.ExecutorCVM {
		if err := s.stageDataFiles(task); err != nil {
			return nil, fmt.Errorf("failed to stage data files: %w", err)
		}
	}

	previousStatus := task.Status
	if task.Executor == model.ExecutorCVM {
		forceNew := !strings.EqualFold(strings.TrimSpace(task.VMStatus), "DISPATCHING")
		if err := s.prepareCVMExecutionAttempt(task, forceNew); err != nil {
			return nil, err
		}
	} else {
		task.ExecutionAttemptID = uuid.New().String()
		task.UpdatedAt = time.Now()
		if err := s.repo.Update(task); err != nil {
			return nil, fmt.Errorf("persist execution attempt: %w", err)
		}
	}
	if err := s.admitTask(ctx, model.OverlayAdmissionActionRetry, actor, task); err != nil {
		return nil, err
	}

	var dispatch *model.CVMDispatchResponse
	if task.Executor == model.ExecutorCVM {
		request, err := s.buildCVMDispatchRequest(ctx, actor, task)
		if err != nil {
			s.markCVMStartFailed(task)
			s.emitTaskEvent(model.OverlayTaskEventStartFailed, actor, task, previousStatus, err.Error())
			return nil, err
		}
		dispatch, err = s.overlay.DispatchCVMTask(ctx, request)
		if err != nil {
			// Preserve this attempt when the dispatch outcome is unknown.
			return nil, err
		}
	}

	task.Status = model.TaskStatusRunning
	task.Progress = 0
	task.Error = ""
	now := time.Now()
	task.StartedAt = &now
	task.UpdatedAt = now
	if dispatch != nil {
		task.CVMInstanceID = dispatch.InstanceID
		task.VMStatus = dispatch.InstanceState
	}

	if err := s.repo.Update(task); err != nil {
		if dispatch != nil {
			_ = s.overlay.CancelCVMTask(context.Background(), model.CVMCancelRequest{Actor: actor, TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, Reason: "Octopus task retry update failed"})
			s.markCVMStartFailed(task)
		}
		s.emitTaskEvent(model.OverlayTaskEventStartFailed, actor, task, previousStatus, err.Error())
		return nil, err
	}

	s.emitTaskEvent(model.OverlayTaskEventRunning, actor, task, previousStatus, "")
	if task.Executor != model.ExecutorCVM {
		go s.launchTask(task)
	}

	return task, nil
}

// launchTask starts miniwdl as a detached process and returns immediately.
// Sepiida Agent monitors the filesystem and reports progress to Sepiida Server.
// The syncLoop periodically updates Octopus DB from Sepiida Server.
func (s *TaskService) launchTask(task *model.Task) {
	// Write input JSON
	inputFile := filepath.Join(s.cfg.Task.OutputDir, task.ID+"_inputs.json")
	inputJSON := []byte(task.InputJSON)
	var executionInputs map[string]interface{}
	if err := json.Unmarshal(inputJSON, &executionInputs); err != nil {
		s.updateTaskError(task, "Failed to parse workflow inputs")
		return
	}
	delete(executionInputs, "reference_genome")
	if task.Template == "trio" {
		inputs := executionInputs
		content, _ := inputs["TrioWES.ped_content"].(string)
		pedPath, _ := inputs["TrioWES.ped"].(string)
		delete(inputs, "TrioWES.ped_content")
		if content == "" || pedPath == "" {
			s.updateTaskError(task, "Trio PED input is missing")
			return
		}
		if err := os.MkdirAll(filepath.Dir(pedPath), 0750); err != nil {
			s.updateTaskError(task, err.Error())
			return
		}
		if err := os.WriteFile(pedPath, []byte(content), 0600); err != nil {
			s.updateTaskError(task, err.Error())
			return
		}
	}
	inputJSON, _ = json.Marshal(executionInputs)
	if err := os.WriteFile(inputFile, inputJSON, 0600); err != nil {
		s.updateTaskError(task, fmt.Sprintf("Failed to write input file: %v", err))
		return
	}

	// Create log directory
	logPath := filepath.Join(s.cfg.Task.OutputDir, task.UUID, "octopus.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0750); err != nil {
		_ = os.Remove(inputFile)
		s.updateTaskError(task, fmt.Sprintf("Failed to create task log directory: %v", err))
		return
	}

	cmd := s.miniWDLCommand(task, inputFile)

	// Detach from parent process
	cmd.SysProcAttr = &syscall.SysProcAttr{}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	// Start (non-blocking) — returns immediately
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		_ = os.Remove(inputFile)
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
		if logFile != nil {
			_ = logFile.Close()
		}

		_ = os.Remove(inputFile)

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
		s.syncCNVBaselineOutput(task, "")
		s.emitStatusEvent(task, previousStatus)
		if task.Status == model.TaskStatusCompleted {
			s.importTaskArchive(task)
		}
	}()
}

func (s *TaskService) miniWDLCommand(task *model.Task, inputFile string) *exec.Cmd {
	return exec.Command(
		s.getExecutorPath(task.Executor), "run",
		filepath.Join(s.cfg.Task.TemplateDir, task.Template+".wdl"),
		"-p", s.cfg.Task.TemplateDir,
		"--cfg", task.ConfigFile,
		"-i", inputFile,
		"-d", filepath.Join(s.cfg.Task.OutputDir, task.UUID),
	)
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

	if s.cfg.Task.ArchiveDir == "" {
		return
	}
	archiveDir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
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
	var batchID uint
	if batch != nil {
		batchID = batch.ID
	}
	result, err := NewImporter(s.cfg).ImportFromTaskArchive(task, archiveDir, batchID)
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
		if task.Executor == model.ExecutorCVM {
			if publishErr := s.publishCVMFinalManifest(task, finishedAt); publishErr != nil {
				task.ResultImportStatus = model.ResultImportStatusFailed
				task.ResultImportError = publishErr.Error()
				task.ResultImportFingerprint = ""
			}
		}
	}
	_ = s.repo.Update(task)
	s.finishResultImportBatch(batch, result, task.ResultImportStatus, task.ResultImportError, finishedAt)
}

func (s *TaskService) startResultImportBatch(task *model.Task, archiveDir, fingerprint string, startedAt time.Time) *model.ResultImportBatch {
	if task == nil || s.importBatchRepo == nil {
		return nil
	}
	attemptID := task.ExecutionAttemptID
	if attemptID == "" {
		attemptID = task.UUID
	}

	batch := &model.ResultImportBatch{
		TaskUUID:           task.UUID,
		TenantID:           model.TenantIDForTask(task),
		ExecutionAttemptID: attemptID,
		Source:             "local",
		Status:             model.ResultImportBatchStatusRunning,
		Fingerprint:        fingerprint,
		ArchiveBase:        s.cfg.Task.ArchiveDir,
		ArchivePrefix:      task.UUID,
		OutputsKey:         "outputs.resolved.json",
		StartedAt:          startedAt,
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

func cvmFinalManifestKey(task *model.Task) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is required")
	}
	if _, err := uuid.Parse(task.ExternalOrgID); err != nil {
		return "", fmt.Errorf("valid organization ID is required for CVM final manifest")
	}
	if _, err := uuid.Parse(task.UUID); err != nil {
		return "", fmt.Errorf("valid task UUID is required for CVM final manifest")
	}
	if _, err := uuid.Parse(task.ExecutionAttemptID); err != nil {
		return "", fmt.Errorf("valid execution attempt ID is required for CVM final manifest")
	}
	return path.Join("organizations", task.ExternalOrgID, "workflows", task.UUID, "final.json"), nil
}

func (s *TaskService) publishCVMFinalManifest(task *model.Task, finalizedAt time.Time) error {
	key, err := cvmFinalManifestKey(task)
	if err != nil {
		return err
	}
	manifest, err := json.Marshal(map[string]interface{}{
		"layout_version": 1,
		"task_uuid":      task.UUID,
		"attempt_id":     task.ExecutionAttemptID,
		"archive_prefix": path.Join("organizations", task.ExternalOrgID, "workflows", task.UUID, "attempts", task.ExecutionAttemptID),
		"finalized_at":   finalizedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return fmt.Errorf("encode CVM final manifest: %w", err)
	}
	storage, err := newS3Storage(context.Background(), s.cfg.Storage)
	if err != nil {
		return err
	}
	if err := storage.put(context.Background(), key, "application/json", append(manifest, '\n')); err != nil {
		return fmt.Errorf("publish CVM final manifest: %w", err)
	}
	return nil
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

	if s.cfg.Task.ArchiveDir == "" {
		return nil, fmt.Errorf("archive directory is not configured")
	}
	archiveDir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
		return nil, err
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
		ExecutionAttemptID:      task.ExecutionAttemptID,
		CreatedAt:               task.CreatedAt,
		ResultImportStatus:      task.ResultImportStatus,
		ResultImportError:       task.ResultImportError,
		ResultImportedAt:        task.ResultImportedAt,
		ResultImportFingerprint: task.ResultImportFingerprint,
		ResultImportAttempts:    task.ResultImportAttempts,
	}

	// Query Sepiida for real-time progress
	if s.sepiida != nil && task.UUID != "" {
		workflow, tasks, err := s.sepiida.GetWorkflowWithTasks(sepiidaWorkflowUUID(task))
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
	task, err := s.repo.FindByUUID(id)
	if err != nil {
		return fmt.Errorf("task not found: %s", id)
	}

	if task.Status != model.TaskStatusRunning && task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusWaitingData {
		return fmt.Errorf("task is not running or queued")
	}
	if cvmTaskNeedsCancel(task) {
		if err := s.overlay.CancelCVMTask(ctx, model.CVMCancelRequest{Actor: actor, TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, Reason: "task cancelled by user"}); err != nil {
			return err
		}
	}

	s.mu.Lock()
	if cmd, ok := s.running[id]; ok {
		_ = cmd.Process.Kill()
		delete(s.running, id)
	}
	s.mu.Unlock()

	task.Status = model.TaskStatusCancelled
	if task.Executor == model.ExecutorCVM {
		task.VMStatus = "TERMINATED"
	}
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
// in waiting_for_data status. When the sample has a complete effective R1/R2
// match and the data is accessible, the task starts automatically.
func (s *TaskService) StartDataWaitSync(ctx context.Context, interval time.Duration) {
	go func() {
		s.checkWaitingDataTasks()
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
			actor := model.OverlayActor{UserID: task.CreatedBy, OrgID: task.ExternalOrgID}
			if _, startErr := s.StartTask(context.Background(), task.UUID, actor); startErr != nil {
				task.Status = model.TaskStatusQueued
				task.Error = "automatic start deferred: " + startErr.Error()
				task.UpdatedAt = time.Now()
				_ = s.repo.Update(&task)
			}
		}
	}
}

func (s *TaskService) checkDataReady(task *model.Task) (ready bool, reason string) {
	var inputs map[string]interface{}
	if task.InputJSON != "" {
		if err := json.Unmarshal([]byte(task.InputJSON), &inputs); err != nil {
			return false, "failed to parse input JSON"
		}
	}
	if inputs == nil {
		inputs = make(map[string]interface{})
	}
	if task.SampleIDRef != 0 {
		sample, err := s.sampleRepo.FindByID(task.SampleIDRef)
		if err != nil {
			return false, "sample not found"
		}
		matchedPair := sample.GetMatchedPair()
		if !matchedPairComplete(matchedPair) {
			return false, "sample is waiting for a complete R1/R2 match"
		}
		if applySampleMatchedPairToInputs(inputs, matchedPair) {
			data, err := json.Marshal(inputs)
			if err != nil {
				return false, "failed to update input JSON"
			}
			task.InputJSON = string(data)
			task.UpdatedAt = time.Now()
			if err := s.repo.Update(task); err != nil {
				return false, "failed to update task inputs"
			}
		}
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
	if task.Executor != model.ExecutorCVM {
		if err := s.stageDataFiles(task); err != nil {
			return false, err.Error()
		}
	}
	if err := json.Unmarshal([]byte(task.InputJSON), &inputs); err != nil {
		return false, err.Error()
	}
	if task.SampleIDRef != 0 {
		if r1, ok := inputs["fastq_r1"].(string); !ok || strings.TrimSpace(r1) == "" {
			return false, "sample R1 data is not ready"
		}
		if r2, ok := inputs["fastq_r2"].(string); !ok || strings.TrimSpace(r2) == "" {
			return false, "sample R2 data is not ready"
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

		if task.Executor == model.ExecutorCVM {
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

func matchedPairComplete(pair *model.MatchedPair) bool {
	return pair != nil && strings.TrimSpace(pair.R1Path) != "" && strings.TrimSpace(pair.R2Path) != ""
}

func applySampleMatchedPairToInputs(inputs map[string]interface{}, pair *model.MatchedPair) bool {
	if inputs == nil || !matchedPairComplete(pair) {
		return false
	}
	changed := false
	for key, value := range map[string]string{"fastq_r1": pair.R1Path, "fastq_r2": pair.R2Path} {
		if current, ok := inputs[key].(string); !ok || current != value {
			inputs[key] = value
			changed = true
		}
	}
	return changed
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
	assets, directLinks, err := s.collectTaskAssets(task)
	if err != nil {
		return err
	}
	var remote []*model.DataAsset
	for _, asset := range assets {
		if asset.Provider == model.UploadProviderS3 {
			remote = append(remote, asset)
		}
	}
	sort.Slice(remote, func(i, j int) bool { return remote[i].UUID < remote[j].UUID })
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
		applyAssetInputPaths(inputs, asset, directLinks[asset.ID], destination)
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	task.InputJSON = string(encoded)
	task.UpdatedAt = time.Now()
	return s.repo.Update(task)
}

func (s *TaskService) collectTaskAssets(task *model.Task) (map[uint]*model.DataAsset, map[uint][]model.TaskDataAsset, error) {
	assets := make(map[uint]*model.DataAsset)
	directLinks := make(map[uint][]model.TaskDataAsset)
	var taskAssets []model.TaskDataAsset
	if err := database.GetDB().Where("task_uuid = ?", task.UUID).Order("input_role, input_index").Find(&taskAssets).Error; err != nil {
		return nil, nil, err
	}
	for _, link := range taskAssets {
		asset, err := s.assetRepo.FindByID(link.AssetID)
		if err != nil || asset.Status != model.FileStatusCompleted {
			return nil, nil, fmt.Errorf("data asset for task input is not ready")
		}
		assets[asset.ID] = asset
		directLinks[asset.ID] = append(directLinks[asset.ID], link)
	}
	if task.SampleIDRef != 0 {
		var link model.SampleDataLink
		db := database.GetDB()
		err := db.Where("sample_id = ? AND match_mode = ?", task.SampleIDRef, model.SampleMatchModeManual).First(&link).Error
		if err != nil {
			err = db.Where("sample_id = ? AND match_mode = ?", task.SampleIDRef, model.SampleMatchModeAutomatic).First(&link).Error
		}
		if err == nil {
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
	for _, asset := range assets {
		if asset.Status != model.FileStatusCompleted {
			return nil, nil, fmt.Errorf("data asset %s is not ready", asset.UUID)
		}
	}
	return assets, directLinks, nil
}

func (s *TaskService) stageCOSArchive(task *model.Task, metadata model.SepiidaArchiveMetadata) error {
	if task == nil || s.cfg.Storage.Provider != "s3" {
		return fmt.Errorf("COS/S3 storage is required for a CVM archive")
	}
	prefix, err := validatedCOSArchivePrefix(task, metadata, s.cfg.Storage)
	if err != nil {
		return err
	}
	storage, err := newS3Storage(context.Background(), s.cfg.Storage)
	if err != nil {
		return err
	}
	objects, err := storage.list(context.Background(), prefix+"/")
	if err != nil {
		return err
	}
	archiveDir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
		return err
	}
	if err := ensurePathInsideBase(s.cfg.Task.ArchiveDir, archiveDir); err != nil {
		return err
	}
	if err := os.MkdirAll(archiveDir, 0750); err != nil {
		return err
	}
	selected := 0
	for _, object := range objects {
		name := path.Base(object.Key)
		if name != "outputs.resolved.json" && !isStructuredResultFile(name) {
			continue
		}
		destination := filepath.Join(archiveDir, name)
		if err := ensurePathInsideBase(archiveDir, destination); err != nil {
			return err
		}
		if info, err := os.Stat(destination); err == nil && info.Mode().IsRegular() && info.Size() == object.Size {
			selected++
			continue
		}
		source, err := storage.open(context.Background(), object.Key)
		if err != nil {
			return err
		}
		temporary := destination + ".part"
		file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			source.Close()
			return err
		}
		written, copyErr := io.Copy(file, source)
		closeErr := file.Close()
		source.Close()
		if copyErr != nil || closeErr != nil || written != object.Size {
			_ = os.Remove(temporary)
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			return fmt.Errorf("COS archive object size mismatch: %s", object.Key)
		}
		if err := os.Rename(temporary, destination); err != nil {
			_ = os.Remove(temporary)
			return err
		}
		selected++
	}
	if selected == 0 {
		return fmt.Errorf("COS archive contains no importable result files")
	}
	if _, err := os.Stat(filepath.Join(archiveDir, "outputs.resolved.json")); err != nil {
		return fmt.Errorf("COS archive is missing outputs.resolved.json")
	}
	return nil
}

func validatedCOSArchivePrefix(task *model.Task, metadata model.SepiidaArchiveMetadata, storageCfg config.StorageConfig) (string, error) {
	if task == nil || task.Executor != model.ExecutorCVM {
		return "", fmt.Errorf("CVM task is required for a COS archive")
	}
	if _, err := uuid.Parse(task.ExternalOrgID); err != nil {
		return "", fmt.Errorf("valid organization ID is required for a COS archive")
	}
	if _, err := uuid.Parse(task.UUID); err != nil {
		return "", fmt.Errorf("valid task UUID is required for a COS archive")
	}
	if _, err := uuid.Parse(task.ExecutionAttemptID); err != nil {
		return "", fmt.Errorf("valid execution attempt ID is required for a COS archive")
	}
	baseURL, err := url.Parse(strings.TrimSpace(metadata.ArchiveBase))
	if err != nil || !strings.EqualFold(baseURL.Scheme, "https") || baseURL.Hostname() == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return "", fmt.Errorf("Sepiida returned an invalid COS archive base")
	}
	expectedHost := storageCfg.S3Bucket + ".cos." + storageCfg.S3Region + ".myqcloud.com"
	if !strings.EqualFold(baseURL.Host, expectedHost) {
		return "", fmt.Errorf("Sepiida archive bucket does not match configured COS bucket")
	}
	prefix := strings.Trim(baseURL.Path, "/")
	expectedBasePrefix := path.Join("organizations", task.ExternalOrgID, "workflows", task.UUID, "attempts")
	if prefix != expectedBasePrefix {
		return "", fmt.Errorf("Sepiida archive prefix does not match the current tenant and task")
	}
	archivePrefix := strings.Trim(strings.TrimSpace(metadata.ArchivePrefix), "/")
	if archivePrefix != task.ExecutionAttemptID {
		return "", fmt.Errorf("Sepiida archive prefix does not match the current execution attempt")
	}
	expectedOutputsKey := path.Join(task.ExecutionAttemptID, "outputs.resolved.json")
	if strings.Trim(strings.TrimSpace(metadata.OutputsResolvedKey), "/") != expectedOutputsKey {
		return "", fmt.Errorf("Sepiida outputs manifest does not match the current execution attempt")
	}
	return path.Join(prefix, archivePrefix), nil
}

func taskArchiveDir(baseDir string, task *model.Task) (string, error) {
	if task == nil || strings.TrimSpace(task.UUID) == "" {
		return "", fmt.Errorf("task UUID is required for archive path")
	}
	if task.Executor != model.ExecutorCVM {
		return filepath.Join(baseDir, task.UUID), nil
	}
	if _, err := uuid.Parse(task.ExecutionAttemptID); err != nil {
		return "", fmt.Errorf("valid execution attempt ID is required for CVM archive path")
	}
	return filepath.Join(baseDir, task.UUID, "attempts", task.ExecutionAttemptID), nil
}

func isStructuredResultFile(name string) bool {
	name = strings.ToLower(name)
	if !strings.HasSuffix(name, ".txt") && !strings.HasSuffix(name, ".tsv") {
		return false
	}
	return strings.Contains(name, "snv_indel") || strings.Contains(name, "snv.indel") ||
		strings.Contains(name, "region.cnvanno") || strings.Contains(name, "gene.cnvanno") ||
		strings.Contains(name, ".str") || strings.Contains(name, "str.txt") ||
		strings.Contains(name, ".mei") || strings.Contains(name, "mei.txt") ||
		strings.Contains(name, "mt_report") || strings.Contains(name, ".mt_") || strings.Contains(name, "roh")
}

func applyAssetInputPaths(inputs map[string]interface{}, asset *model.DataAsset, links []model.TaskDataAsset, value string) {
	if len(links) > 0 {
		for _, link := range links {
			applyTaskAssetPath(inputs, link, value)
		}
		return
	}
	switch asset.ReadType {
	case model.ReadTypeRead1:
		inputs["fastq_r1"] = value
	case model.ReadTypeRead2:
		inputs["fastq_r2"] = value
	case model.ReadTypeSingle:
		inputs["fastq_r1"] = value
	case model.ReadTypeBed:
		inputs["bed_file"] = value
	}
}

func (s *TaskService) buildCVMDispatchRequest(ctx context.Context, actor model.OverlayActor, task *model.Task) (model.CVMDispatchRequest, error) {
	if _, err := uuid.Parse(task.ExecutionAttemptID); err != nil {
		return model.CVMDispatchRequest{}, fmt.Errorf("valid execution attempt ID is required for CVM dispatch")
	}
	assets, directLinks, err := s.collectTaskAssets(task)
	if err != nil {
		return model.CVMDispatchRequest{}, err
	}
	remote := make([]*model.DataAsset, 0, len(assets))
	for _, asset := range assets {
		if asset.Provider != model.UploadProviderS3 {
			return model.CVMDispatchRequest{}, fmt.Errorf("CVM input asset %s is not stored in COS/S3", asset.UUID)
		}
		remote = append(remote, asset)
	}
	sort.Slice(remote, func(i, j int) bool { return remote[i].UUID < remote[j].UUID })

	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(task.InputJSON), &inputs); err != nil {
		return model.CVMDispatchRequest{}, fmt.Errorf("parse CVM task inputs: %w", err)
	}
	referenceGenome, err := cvmReferenceGenome(inputs)
	if err != nil {
		return model.CVMDispatchRequest{}, err
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return model.CVMDispatchRequest{}, err
	}
	downloads := make([]model.CVMInputDownload, 0, len(remote))
	for _, asset := range remote {
		name := safeCVMInputName(asset.FileName)
		target := path.Join("/mnt/data/inputs", asset.UUID+"-"+name)
		url, err := storage.presignDownload(ctx, asset.StorageKey, name)
		if err != nil {
			return model.CVMDispatchRequest{}, fmt.Errorf("presign CVM input %s: %w", asset.UUID, err)
		}
		inputs = replaceInputString(inputs, asset.StorageKey, target).(map[string]interface{})
		applyAssetInputPaths(inputs, asset, directLinks[asset.ID], target)
		downloads = append(downloads, model.CVMInputDownload{ObjectKey: asset.StorageKey, URL: url, Target: target})
	}
	inputs, err = buildCVMWDLInputs(task.Template, referenceGenome, inputs)
	if err != nil {
		return model.CVMDispatchRequest{}, err
	}
	inlineFiles := make([]model.CVMInlineFile, 0, 1)
	if task.Template == "trio" {
		content, _ := inputs["TrioWES.ped_content"].(string)
		if content == "" {
			return model.CVMDispatchRequest{}, fmt.Errorf("Trio PED content is missing")
		}
		target := path.Join("/mnt/data/inputs", taskOutputPrefix(task.UUID)+".ped")
		inputs["TrioWES.ped"] = target
		delete(inputs, "TrioWES.ped_content")
		inlineFiles = append(inlineFiles, model.CVMInlineFile{Target: target, Content: content})
	}
	if task.Template == "single" || task.Template == "trio" {
		thresholds, configErr := NewWorkflowConfigService().Get(task.Template, referenceGenome)
		if configErr != nil {
			return model.CVMDispatchRequest{}, configErr
		}
		applyWorkflowThresholds(inputs, thresholds)
	}
	delete(inputs, "reference_genome")
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return model.CVMDispatchRequest{}, err
	}
	return model.CVMDispatchRequest{
		Actor:     actor,
		Task:      model.NewOverlayTaskSnapshot(task),
		AttemptID: task.ExecutionAttemptID,
		Execution: model.CVMExecutionSpec{
			Template: task.Template, ReferenceGenome: referenceGenome, Inputs: encoded, Downloads: downloads, InlineFiles: inlineFiles,
		},
		RequestedAt: time.Now(),
	}, nil
}

func buildCVMWDLInputs(template, genome string, taskInputs map[string]interface{}) (map[string]interface{}, error) {
	inputs, err := workflow.InputsForGenome(template, genome)
	if err != nil {
		return nil, err
	}
	for key, value := range taskInputs {
		if _, known := inputs[key]; known {
			inputs[key] = value
		}
	}
	inputs["reference_genome"] = genome
	if content, ok := taskInputs["TrioWES.ped_content"]; ok {
		inputs["TrioWES.ped_content"] = content
	}

	switch template {
	case "single":
		copyCVMInput(inputs, "SingleWES.read_1", taskInputs, "fastq_r1")
		copyCVMInput(inputs, "SingleWES.read_2", taskInputs, "fastq_r2")
		copyCVMInput(inputs, "SingleWES.bed", taskInputs, "bed_file")
		copyCVMInput(inputs, "SingleWES.cnvkit_reference", taskInputs, "cnv_baseline")
	case "trio":
		copyCVMInput(inputs, "TrioWES.bed", taskInputs, "bed_file")
	case "baseline_fix":
		copyCVMInput(inputs, "CNVBaselineFix.bed", taskInputs, "bed_file")
	}
	return inputs, nil
}

func (s *TaskService) prepareTrioInputs(pedigreeID, workflowUUID string, actor model.OverlayActor, inputs map[string]interface{}) (*model.Sample, []model.TaskDataAsset, error) {
	pedigree, members, err := s.pedigreeRepo.FindByIDWithMembers(pedigreeID)
	if err != nil || (actor.Role != string(model.SystemRoleSuperAdmin) && pedigree.CreatedBy != actor.UserID) {
		return nil, nil, fmt.Errorf("pedigree not found: %s", pedigreeID)
	}
	byID := make(map[string]model.PedigreeMember, len(members))
	var proband *model.PedigreeMember
	for i := range members {
		member := members[i]
		byID[member.ID] = member
		if member.ID == pedigree.ProbandMemberID || member.Relation == model.RelationProband {
			if proband != nil && proband.ID != member.ID {
				return nil, nil, fmt.Errorf("trio pedigree must contain exactly one proband")
			}
			copy := member
			proband = &copy
		}
	}
	if proband == nil {
		return nil, nil, fmt.Errorf("trio pedigree has no proband")
	}
	father, fatherOK := byID[proband.FatherID]
	mother, motherOK := byID[proband.MotherID]
	if !fatherOK || !motherOK || father.Relation != model.RelationFather || mother.Relation != model.RelationMother {
		return nil, nil, fmt.Errorf("trio proband must reference one father and one mother")
	}
	ordered := []model.PedigreeMember{*proband, father, mother}
	read1 := make([]string, 3)
	read2 := make([]string, 3)
	assets := make([]model.TaskDataAsset, 0, 6)
	var probandSample *model.Sample
	for i, member := range ordered {
		if strings.TrimSpace(member.SampleID) == "" {
			return nil, nil, fmt.Errorf("trio member %s has no linked sample", member.Name)
		}
		sample, err := s.sampleRepo.FindScopedByUUID(member.SampleID, actor)
		if err != nil {
			return nil, nil, fmt.Errorf("linked sample for trio member %s was not found", member.Name)
		}
		pair := sample.GetMatchedPair()
		if !matchedPairComplete(pair) {
			return nil, nil, fmt.Errorf("trio member %s requires complete R1/R2 data", member.Name)
		}
		read1[i], read2[i] = pair.R1Path, pair.R2Path
		for _, item := range []struct{ storage, role string }{{pair.R1Path, model.TaskAssetRoleTrioRead1}, {pair.R2Path, model.TaskAssetRoleTrioRead2}} {
			asset, err := s.assetRepo.FindCompletedByStorageKey(item.storage, actor)
			if err != nil {
				return nil, nil, fmt.Errorf("trio member %s data asset is unavailable", member.Name)
			}
			assets = append(assets, model.TaskDataAsset{AssetID: asset.ID, InputRole: item.role, InputIndex: i})
		}
		if i == 0 {
			probandSample = sample
		}
	}
	prefix := taskOutputPrefix(workflowUUID)
	inputs["TrioWES.meta_info"] = map[string]interface{}{"members": []string{"proband", "father", "mother"}, "read_1": read1, "read_2": read2}
	inputs["TrioWES.ped_content"] = trioPED(prefix, ordered)
	return probandSample, assets, nil
}

func taskOutputPrefix(workflowUUID string) string {
	return workflowUUID
}

func trioPED(familyID string, members []model.PedigreeMember) string {
	sex := func(g model.Gender) int {
		if g == model.GenderMale {
			return 1
		}
		if g == model.GenderFemale {
			return 2
		}
		return 0
	}
	phenotype := func(a model.AffectedStatus) int {
		if a == model.AffectedStatusAffected {
			return 2
		}
		if a == model.AffectedStatusUnaffected {
			return 1
		}
		return 0
	}
	return fmt.Sprintf("%s\tproband\tfather\tmother\t%d\t%d\n%s\tfather\t0\t0\t%d\t%d\n%s\tmother\t0\t0\t%d\t%d\n", familyID, sex(members[0].Gender), phenotype(members[0].AffectedStatus), familyID, sex(members[1].Gender), phenotype(members[1].AffectedStatus), familyID, sex(members[2].Gender), phenotype(members[2].AffectedStatus))
}

func copyCVMInput(target map[string]interface{}, targetKey string, source map[string]interface{}, sourceKey string) {
	if value, ok := source[sourceKey]; ok {
		target[targetKey] = value
	}
}

func cvmReferenceGenome(inputs map[string]interface{}) (string, error) {
	value, _ := inputs["reference_genome"].(string)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "hg19", "grch37":
		return "hg19", nil
	case "hg38", "grch38":
		return "hg38", nil
	default:
		return "", fmt.Errorf("CVM reference_genome must be hg19/GRCh37 or hg38/GRCh38")
	}
}

func (s *TaskService) prepareCVMExecutionAttempt(task *model.Task, forceNew bool) error {
	if task == nil || task.Executor != model.ExecutorCVM {
		return fmt.Errorf("CVM task is required")
	}
	_, invalidAttempt := uuid.Parse(task.ExecutionAttemptID)
	if forceNew || invalidAttempt != nil || cvmAttemptStateTerminal(task.VMStatus) {
		task.ExecutionAttemptID = uuid.New().String()
		task.CVMInstanceID = ""
	}
	task.CVMArchiveStagedAt = nil
	task.CVMArchiveTerminationNotifiedAt = nil
	task.VMStatus = "DISPATCHING"
	task.UpdatedAt = time.Now()
	if err := s.repo.Update(task); err != nil {
		return fmt.Errorf("persist CVM execution attempt: %w", err)
	}
	return nil
}

func (s *TaskService) markCVMStartFailed(task *model.Task) {
	if task == nil || task.Executor != model.ExecutorCVM {
		return
	}
	task.VMStatus = "LAUNCH_FAILED"
	task.UpdatedAt = time.Now()
	_ = s.repo.Update(task)
}

func cvmAttemptStateTerminal(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "TERMINATED", "SHUTDOWN", "FAILED", "RECLAIMED", "STOPPED", "LAUNCH_FAILED":
		return true
	default:
		return false
	}
}

func replaceInputString(value interface{}, oldValue, newValue string) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			typed[key] = replaceInputString(item, oldValue, newValue)
		}
	case []interface{}:
		for i, item := range typed {
			typed[i] = replaceInputString(item, oldValue, newValue)
		}
	case string:
		if typed == oldValue {
			return newValue
		}
	}
	return value
}

func safeCVMInputName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	var clean strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			clean.WriteRune(r)
		} else {
			clean.WriteByte('_')
		}
	}
	if clean.Len() == 0 || clean.String() == "." || clean.String() == ".." {
		return "input.dat"
	}
	return clean.String()
}

func (s *TaskService) HandleCVMStateEvent(event model.CVMStateEvent) error {
	if strings.TrimSpace(event.TaskUUID) == "" || strings.TrimSpace(event.AttemptID) == "" || strings.TrimSpace(event.InstanceState) == "" {
		return fmt.Errorf("task_uuid, attempt_id and instance_state are required")
	}
	task, err := s.repo.FindByUUID(event.TaskUUID)
	if err != nil {
		return fmt.Errorf("task not found: %s", event.TaskUUID)
	}
	if task.Executor != model.ExecutorCVM {
		return fmt.Errorf("task is not a CVM task")
	}
	if !cvmStateEventMatchesCurrentAttempt(task, event) {
		return nil
	}
	if task.CVMInstanceID != "" && event.InstanceID != "" && task.CVMInstanceID != event.InstanceID {
		return fmt.Errorf("CVM instance does not match task")
	}
	previousStatus := task.Status
	if event.InstanceID != "" {
		task.CVMInstanceID = event.InstanceID
	}
	task.VMStatus = strings.ToUpper(event.InstanceState)
	if event.TaskStatus == model.TaskStatusFailed && task.Status != model.TaskStatusCompleted && task.Status != model.TaskStatusCancelled {
		task.Status = model.TaskStatusFailed
		task.Error = strings.TrimSpace(event.Message)
		if task.Error == "" {
			task.Error = "CVM instance terminated before the workflow completed"
		}
		now := time.Now()
		task.FinishedAt = &now
	}
	task.UpdatedAt = time.Now()
	if err := s.repo.Update(task); err != nil {
		return err
	}
	s.emitStatusEvent(task, previousStatus)
	return nil
}

func cvmStateEventMatchesCurrentAttempt(task *model.Task, event model.CVMStateEvent) bool {
	return task != nil && task.ExecutionAttemptID != "" && task.ExecutionAttemptID == event.AttemptID
}

func cvmTaskNeedsCancel(task *model.Task) bool {
	return task != nil && task.Executor == model.ExecutorCVM && strings.TrimSpace(task.ExecutionAttemptID) != ""
}

func setIndexedInput(values []string, index int, value string) []string {
	if index < 0 {
		index = 0
	}
	for len(values) <= index {
		values = append(values, "")
	}
	values[index] = value
	return values
}

func applyTaskAssetPath(inputs map[string]interface{}, link model.TaskDataAsset, value string) {
	switch link.InputRole {
	case model.TaskAssetRoleTrioRead1, model.TaskAssetRoleTrioRead2:
		meta, _ := inputs["TrioWES.meta_info"].(map[string]interface{})
		if meta == nil {
			meta = map[string]interface{}{}
		}
		key := "read_1"
		if link.InputRole == model.TaskAssetRoleTrioRead2 {
			key = "read_2"
		}
		values := make([]string, 0)
		switch current := meta[key].(type) {
		case []string:
			values = append(values, current...)
		case []interface{}:
			for _, item := range current {
				values = append(values, fmt.Sprint(item))
			}
		}
		meta[key] = setIndexedInput(values, link.InputIndex, value)
		inputs["TrioWES.meta_info"] = meta
	case model.TaskAssetRoleCNVRead1, model.TaskAssetRoleCNVRead2:
		key := "CNVBaselineFix.read_1"
		if link.InputRole == model.TaskAssetRoleCNVRead2 {
			key = "CNVBaselineFix.read_2"
		}
		values := make([]string, 0)
		switch current := inputs[key].(type) {
		case []string:
			values = append(values, current...)
		case []interface{}:
			for _, item := range current {
				values = append(values, fmt.Sprint(item))
			}
		}
		inputs[key] = setIndexedInput(values, link.InputIndex, value)
	case model.TaskAssetRoleCNVBED:
		inputs["CNVBaselineFix.bed"] = value
	case model.TaskAssetRoleAnalysisBED:
		inputs["bed_file"] = value
	}
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
