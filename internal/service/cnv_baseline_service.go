package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
)

type CNVBaselineService struct {
	repo    *repository.CNVBaselineRepository
	assets  *repository.DataAssetRepository
	tasks   *repository.TaskRepository
	taskSvc *TaskService
}

func NewCNVBaselineService(cfg *config.Config) *CNVBaselineService {
	return &CNVBaselineService{
		repo: repository.NewCNVBaselineRepository(), assets: repository.NewDataAssetRepository(),
		tasks: repository.NewTaskRepository(), taskSvc: NewTaskService(cfg),
	}
}

func validateBaselineGenome(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "GRCH37", "HG19":
		return model.ReferenceGenomeGRCh37, nil
	case "GRCH38", "HG38":
		return model.ReferenceGenomeGRCh38, nil
	default:
		return "", fmt.Errorf("reference_genome must be GRCh37 or GRCh38")
	}
}

func (s *CNVBaselineService) scopedCompletedAsset(uuid string, actor model.OverlayActor, readType model.ReadType) (*model.DataAsset, error) {
	asset, err := s.assets.FindScopedByUUID(strings.TrimSpace(uuid), actor)
	if err != nil || asset.Status != model.FileStatusCompleted || asset.ReadType != readType {
		return nil, fmt.Errorf("completed %s data asset not found: %s", readType, uuid)
	}
	return asset, nil
}

func baselinePrefix(value string) string {
	value = regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(strings.TrimSpace(value), "_")
	value = strings.Trim(value, "._-")
	if value == "" {
		return "cnv_baseline"
	}
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}

func (s *CNVBaselineService) Create(ctx context.Context, req *model.CNVBaselineCreateRequest, actor model.OverlayActor) (*model.CNVBaselineResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	genome, err := validateBaselineGenome(req.ReferenceGenome)
	if err != nil {
		return nil, err
	}
	if len(req.Read1AssetIDs) == 0 || len(req.Read1AssetIDs) != len(req.Read2AssetIDs) {
		return nil, fmt.Errorf("read1_asset_ids and read2_asset_ids must contain the same non-zero number of files")
	}
	bed, err := s.scopedCompletedAsset(req.BEDAssetID, actor, model.ReadTypeBed)
	if err != nil {
		return nil, err
	}
	if bed.ReferenceGenome != genome {
		return nil, fmt.Errorf("BED reference genome does not match %s", genome)
	}

	inputAssets := []model.TaskInputAssetRequest{{AssetID: bed.ID, InputRole: model.TaskAssetRoleCNVBED, Index: 0}}
	pairs := make([]model.CNVBaselineReadPair, len(req.Read1AssetIDs))
	seen := make(map[string]bool)
	for i := range req.Read1AssetIDs {
		r1ID, r2ID := strings.TrimSpace(req.Read1AssetIDs[i]), strings.TrimSpace(req.Read2AssetIDs[i])
		if r1ID == "" || r2ID == "" || seen[r1ID] || seen[r2ID] {
			return nil, fmt.Errorf("each selected R1/R2 data asset must be unique")
		}
		seen[r1ID], seen[r2ID] = true, true
		r1, err := s.scopedCompletedAsset(r1ID, actor, model.ReadTypeRead1)
		if err != nil {
			return nil, err
		}
		r2, err := s.scopedCompletedAsset(r2ID, actor, model.ReadTypeRead2)
		if err != nil {
			return nil, err
		}
		pairs[i] = model.CNVBaselineReadPair{PairIndex: i, Read1AssetID: r1.ID, Read2AssetID: r2.ID}
		inputAssets = append(inputAssets,
			model.TaskInputAssetRequest{AssetID: r1.ID, InputRole: model.TaskAssetRoleCNVRead1, Index: i},
			model.TaskInputAssetRequest{AssetID: r2.ID, InputRole: model.TaskAssetRoleCNVRead2, Index: i},
		)
	}

	task, err := s.taskSvc.CreateTask(ctx, &model.TaskCreateRequest{
		PipelineName: "CNV 基线建立流程", PipelineVersion: "builtin",
		Template: "baseline", DeferStart: true, InputAssets: inputAssets,
		Inputs: map[string]interface{}{
			"CNVBaseline.prefix":   baselinePrefix(name),
			"CNVBaseline.assembly": genome,
		},
	}, actor)
	if err != nil {
		return nil, err
	}

	baseline := &model.CNVBaseline{
		UUID: uuid.New().String(), Name: name, ReferenceGenome: genome, BEDAssetID: bed.ID,
		TaskUUID: task.UUID, ExternalOrgID: actor.OrgID, CreatedBy: actor.UserID,
	}
	if err := s.repo.Create(baseline, pairs); err != nil {
		return nil, fmt.Errorf("failed to save CNV baseline task: %w", err)
	}
	if started, startErr := s.taskSvc.StartTask(ctx, task.UUID, actor); startErr == nil {
		task = started
	}
	return s.toResponse(baseline, task)
}

func (s *CNVBaselineService) List(actor model.OverlayActor) ([]model.CNVBaselineResponse, error) {
	baselines, err := s.repo.List(actor)
	if err != nil {
		return nil, err
	}
	items := make([]model.CNVBaselineResponse, 0, len(baselines))
	for i := range baselines {
		task, err := s.tasks.FindByUUID(baselines[i].TaskUUID)
		if err != nil {
			continue
		}
		item, err := s.toResponse(&baselines[i], task)
		if err == nil {
			items = append(items, *item)
		}
	}
	return items, nil
}

func (s *CNVBaselineService) Get(uuid string, actor model.OverlayActor) (*model.CNVBaselineResponse, error) {
	baseline, err := s.repo.FindScopedByUUID(uuid, actor)
	if err != nil {
		return nil, fmt.Errorf("CNV baseline not found")
	}
	task, err := s.tasks.FindByUUID(baseline.TaskUUID)
	if err != nil {
		return nil, err
	}
	return s.toResponse(baseline, task)
}

func (s *CNVBaselineService) toResponse(baseline *model.CNVBaseline, task *model.Task) (*model.CNVBaselineResponse, error) {
	bed, err := s.assets.FindByID(baseline.BEDAssetID)
	if err != nil {
		return nil, err
	}
	pairs, err := s.repo.FindPairs(baseline.ID)
	if err != nil {
		return nil, err
	}
	readPairs := make([][2]model.CNVBaselineAssetResponse, 0, len(pairs))
	for _, pair := range pairs {
		r1, r1Err := s.assets.FindByID(pair.Read1AssetID)
		r2, r2Err := s.assets.FindByID(pair.Read2AssetID)
		if r1Err != nil || r2Err != nil {
			continue
		}
		readPairs = append(readPairs, [2]model.CNVBaselineAssetResponse{{ID: r1.UUID, FileName: r1.FileName}, {ID: r2.UUID, FileName: r2.FileName}})
	}
	return &model.CNVBaselineResponse{
		ID: baseline.UUID, Name: baseline.Name, ReferenceGenome: baseline.ReferenceGenome,
		BED: model.CNVBaselineAssetResponse{ID: bed.UUID, FileName: bed.FileName}, ReadPairs: readPairs,
		TaskID: task.UUID, Status: task.Status, Progress: task.Progress, OutputPath: baseline.OutputPath,
		Error: task.Error, CreatedAt: baseline.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: baseline.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}
