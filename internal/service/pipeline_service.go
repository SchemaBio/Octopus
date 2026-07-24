package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
)

// PipelineService handles organization-scoped custom pipelines and the four
// immutable built-in WES pipelines.
type PipelineService struct {
	repo      *repository.PipelineRepository
	assets    *repository.DataAssetRepository
	baselines *repository.CNVBaselineRepository
	tasks     *repository.TaskRepository
}

func NewPipelineService(_ *config.Config) *PipelineService {
	return &PipelineService{
		repo:      repository.NewPipelineRepository(),
		assets:    repository.NewDataAssetRepository(),
		baselines: repository.NewCNVBaselineRepository(),
		tasks:     repository.NewTaskRepository(),
	}
}

func builtinPipelineResponses() []model.PipelineResponse {
	return []model.PipelineResponse{
		{
			ID: model.BuiltinPipelineWESSingleID, Name: "WES单样本分析",
			BasePipelineID: model.BuiltinPipelineWESSingleID,
			BaseType:       model.PipelineBaseWESSingle, Version: "builtin-v1",
			Description: "系统内置 hg19 单样本 WES 分析流程", BEDFile: "内置 WES BED（hg19，占位）",
			BEDAssetID: model.BuiltinBEDHG19ID, ReferenceGenome: "hg19",
			CNVBaseline: "内置 WES CNV 基线（hg19，占位）", CNVBaselineID: model.BuiltinCNVBaselineHG19ID,
			Template: "single", IsBuiltin: true, Status: model.PipelineStatusActive,
		},
		{
			ID: model.BuiltinPipelineWESFamilyID, Name: "WES家系分析",
			BasePipelineID: model.BuiltinPipelineWESFamilyID,
			BaseType:       model.PipelineBaseWESFamily, Version: "builtin-v1",
			Description: "系统内置 hg19 家系 WES 分析流程", BEDFile: "内置 WES BED（hg19，占位）",
			BEDAssetID: model.BuiltinBEDHG19ID, ReferenceGenome: "hg19",
			CNVBaseline: "内置 WES CNV 基线（hg19，占位）", CNVBaselineID: model.BuiltinCNVBaselineHG19ID,
			Template: "trio", IsBuiltin: true, Status: model.PipelineStatusActive,
		},
		{
			ID: model.BuiltinPipelineWESSingleHG38ID, Name: "WES单样本分析（hg38）",
			BasePipelineID: model.BuiltinPipelineWESSingleHG38ID,
			BaseType:       model.PipelineBaseWESSingle, Version: "builtin-v1",
			Description: "系统内置 hg38 单样本 WES 分析流程", BEDFile: "内置 WES BED（hg38，占位）",
			BEDAssetID: model.BuiltinBEDHG38ID, ReferenceGenome: "hg38",
			CNVBaseline: "内置 WES CNV 基线（hg38，占位）", CNVBaselineID: model.BuiltinCNVBaselineHG38ID,
			Template: "single", IsBuiltin: true, Status: model.PipelineStatusActive,
		},
		{
			ID: model.BuiltinPipelineWESFamilyHG38ID, Name: "WES家系分析（hg38）",
			BasePipelineID: model.BuiltinPipelineWESFamilyHG38ID,
			BaseType:       model.PipelineBaseWESFamily, Version: "builtin-v1",
			Description: "系统内置 hg38 家系 WES 分析流程", BEDFile: "内置 WES BED（hg38，占位）",
			BEDAssetID: model.BuiltinBEDHG38ID, ReferenceGenome: "hg38",
			CNVBaseline: "内置 WES CNV 基线（hg38，占位）", CNVBaselineID: model.BuiltinCNVBaselineHG38ID,
			Template: "trio", IsBuiltin: true, Status: model.PipelineStatusActive,
		},
	}
}

func isAllowedPipelineBase(value model.PipelineBaseType) bool {
	return value == model.PipelineBaseWESSingle || value == model.PipelineBaseWESFamily
}

func resolvePipelineBase(basePipelineID string, legacyBase model.PipelineBaseType, legacyGenome string) (model.PipelineBaseType, string, error) {
	switch strings.TrimSpace(basePipelineID) {
	case model.BuiltinPipelineWESSingleID:
		return model.PipelineBaseWESSingle, "hg19", nil
	case model.BuiltinPipelineWESFamilyID:
		return model.PipelineBaseWESFamily, "hg19", nil
	case model.BuiltinPipelineWESSingleHG38ID:
		return model.PipelineBaseWESSingle, "hg38", nil
	case model.BuiltinPipelineWESFamilyHG38ID:
		return model.PipelineBaseWESFamily, "hg38", nil
	case "":
		if !isAllowedPipelineBase(legacyBase) {
			return "", "", fmt.Errorf("base_pipeline_id must reference a built-in WES pipeline")
		}
		genome, err := normalizePipelineGenome(legacyGenome)
		return legacyBase, genome, err
	default:
		return "", "", fmt.Errorf("base_pipeline_id must reference a built-in WES pipeline")
	}
}

func normalizePipelineGenome(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hg19", "grch37", "":
		return "hg19", nil
	case "hg38", "grch38":
		return "hg38", nil
	default:
		return "", fmt.Errorf("reference_genome must be hg19/GRCh37 or hg38/GRCh38")
	}
}

func genomeMatchesPipeline(value, pipelineGenome string) bool {
	normalized, err := normalizePipelineGenome(value)
	return err == nil && normalized == pipelineGenome
}

func (s *PipelineService) resolveResources(reqBED, reqBaseline string, genome string, actor model.OverlayActor) (*model.DataAsset, *model.CNVBaseline, error) {
	var bed *model.DataAsset
	reqBED = strings.TrimSpace(reqBED)
	if reqBED == model.BuiltinBEDResourceID(genome) {
		reqBED = ""
	}
	if strings.TrimSpace(reqBED) != "" {
		item, err := s.assets.FindScopedByUUID(strings.TrimSpace(reqBED), actor)
		if err != nil || item.Status != model.FileStatusCompleted || item.ReadType != model.ReadTypeBed {
			return nil, nil, fmt.Errorf("completed BED data asset not found")
		}
		if !genomeMatchesPipeline(item.ReferenceGenome, genome) {
			return nil, nil, fmt.Errorf("BED reference genome does not match %s", genome)
		}
		bed = item
	}

	var baseline *model.CNVBaseline
	reqBaseline = strings.TrimSpace(reqBaseline)
	if reqBaseline == model.BuiltinCNVBaselineResourceID(genome) {
		reqBaseline = ""
	}
	if strings.TrimSpace(reqBaseline) != "" {
		item, err := s.baselines.FindScopedByUUID(strings.TrimSpace(reqBaseline), actor)
		if err != nil || strings.TrimSpace(item.OutputPath) == "" {
			return nil, nil, fmt.Errorf("completed CNV baseline not found")
		}
		task, err := s.tasks.FindByUUID(item.TaskUUID)
		if err != nil || task.Status != model.TaskStatusCompleted {
			return nil, nil, fmt.Errorf("completed CNV baseline not found")
		}
		if !genomeMatchesPipeline(item.ReferenceGenome, genome) {
			return nil, nil, fmt.Errorf("CNV baseline reference genome does not match %s", genome)
		}
		baseline = item
	}
	return bed, baseline, nil
}

func (s *PipelineService) CreatePipeline(_ context.Context, req *model.PipelineCreateRequest, actor model.OverlayActor) (*model.PipelineResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if s.repo.ExistsByName(name, actor, "") {
		return nil, fmt.Errorf("pipeline name already exists")
	}
	baseType, genome, err := resolvePipelineBase(req.BasePipelineID, req.BaseType, req.ReferenceGenome)
	if err != nil {
		return nil, err
	}
	bed, baseline, err := s.resolveResources(req.BEDAssetID, req.CNVBaselineID, genome, actor)
	if err != nil {
		return nil, err
	}

	pipeline := &model.Pipeline{
		ID: uuid.New().String(), Name: name, BaseType: baseType,
		Version: strings.TrimSpace(req.Version), Description: strings.TrimSpace(req.Description),
		ReferenceGenome: genome, Status: model.PipelineStatusActive,
		ExternalOrgID: actor.OrgID, CreatedBy: actor.UserID,
	}
	if pipeline.Version == "" {
		pipeline.Version = "v1.0.0"
	}
	if bed != nil {
		pipeline.BEDAssetID = &bed.ID
	}
	if baseline != nil {
		pipeline.CNVBaselineID = &baseline.ID
	}
	if err := s.repo.Create(pipeline); err != nil {
		return nil, err
	}
	response := s.toResponse(pipeline, bed, baseline)
	return &response, nil
}

func (s *PipelineService) GetPipeline(_ context.Context, id string, actor model.OverlayActor) (*model.PipelineResponse, error) {
	for _, item := range builtinPipelineResponses() {
		if item.ID == id {
			return &item, nil
		}
	}
	pipeline, err := s.repo.FindScopedByUUID(id, actor)
	if err != nil {
		return nil, err
	}
	response := s.toResponse(pipeline, nil, nil)
	return &response, nil
}

func (s *PipelineService) ListPipelines(_ context.Context, query *model.PipelineListQuery) (*model.PipelineListResponse, error) {
	pipelines, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}
	items := builtinPipelineResponses()
	for i := range pipelines {
		items = append(items, s.toResponse(&pipelines[i], nil, nil))
	}
	return &model.PipelineListResponse{Total: total + int64(len(builtinPipelineResponses())), Items: items}, nil
}

func (s *PipelineService) UpdatePipeline(_ context.Context, id string, req *model.PipelineUpdateRequest, actor model.OverlayActor) (*model.PipelineResponse, error) {
	if model.IsBuiltinPipelineID(id) {
		return nil, fmt.Errorf("built-in pipelines cannot be modified")
	}
	pipeline, err := s.repo.FindScopedByUUID(id, actor)
	if err != nil {
		return nil, err
	}
	baseType, genome := pipeline.BaseType, pipeline.ReferenceGenome
	if strings.TrimSpace(req.BasePipelineID) != "" {
		baseType, genome, err = resolvePipelineBase(req.BasePipelineID, "", "")
		if err != nil {
			return nil, err
		}
	} else if req.BaseType != "" || strings.TrimSpace(req.ReferenceGenome) != "" {
		legacyBase := req.BaseType
		if legacyBase == "" {
			legacyBase = pipeline.BaseType
		}
		legacyGenome := req.ReferenceGenome
		if strings.TrimSpace(legacyGenome) == "" {
			legacyGenome = pipeline.ReferenceGenome
		}
		baseType, genome, err = resolvePipelineBase("", legacyBase, legacyGenome)
		if err != nil {
			return nil, err
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = pipeline.Name
	}
	if s.repo.ExistsByName(name, actor, id) {
		return nil, fmt.Errorf("pipeline name already exists")
	}
	bed, baseline, err := s.resolveResources(req.BEDAssetID, req.CNVBaselineID, genome, actor)
	if err != nil {
		return nil, err
	}

	pipeline.Name, pipeline.BaseType, pipeline.ReferenceGenome = name, baseType, genome
	pipeline.Description = strings.TrimSpace(req.Description)
	if strings.TrimSpace(req.Version) != "" {
		pipeline.Version = strings.TrimSpace(req.Version)
	}
	pipeline.BEDAssetID, pipeline.CNVBaselineID = nil, nil
	if bed != nil {
		pipeline.BEDAssetID = &bed.ID
	}
	if baseline != nil {
		pipeline.CNVBaselineID = &baseline.ID
	}
	if req.Status == model.PipelineStatusActive || req.Status == model.PipelineStatusInactive {
		pipeline.Status = req.Status
	}
	pipeline.UpdatedAt = time.Now()
	if err := s.repo.Update(pipeline); err != nil {
		return nil, err
	}
	response := s.toResponse(pipeline, bed, baseline)
	return &response, nil
}

func (s *PipelineService) DeletePipeline(_ context.Context, id string, actor model.OverlayActor) error {
	if model.IsBuiltinPipelineID(id) {
		return fmt.Errorf("built-in pipelines cannot be deleted")
	}
	pipeline, err := s.repo.FindScopedByUUID(id, actor)
	if err != nil {
		return err
	}
	return s.repo.DeleteByID(pipeline.ID)
}

func (s *PipelineService) toResponse(pipeline *model.Pipeline, bed *model.DataAsset, baseline *model.CNVBaseline) model.PipelineResponse {
	response := pipeline.ToResponse()
	if pipeline.BEDAssetID != nil {
		if bed == nil {
			bed, _ = s.assets.FindByID(*pipeline.BEDAssetID)
		}
		if bed != nil {
			response.BEDAssetID, response.BEDFile = bed.UUID, bed.FileName
		}
	}
	if pipeline.CNVBaselineID != nil {
		if baseline == nil {
			baseline, _ = s.baselines.FindByID(*pipeline.CNVBaselineID)
		}
		if baseline != nil {
			response.CNVBaselineID, response.CNVBaseline = baseline.UUID, baseline.Name
		}
	}
	if response.BEDFile == "" {
		response.BEDAssetID = model.BuiltinBEDResourceID(pipeline.ReferenceGenome)
		response.BEDFile = fmt.Sprintf("内置 WES BED（%s，占位）", pipeline.ReferenceGenome)
	}
	if response.CNVBaseline == "" {
		response.CNVBaselineID = model.BuiltinCNVBaselineResourceID(pipeline.ReferenceGenome)
		response.CNVBaseline = fmt.Sprintf("内置 WES CNV 基线（%s，占位）", pipeline.ReferenceGenome)
	}
	return response
}
