package service

import (
	"context"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
)

// PipelineService handles pipeline business logic
type PipelineService struct {
	cfg  *config.Config
	repo *repository.PipelineRepository
}

// NewPipelineService creates a new pipeline service
func NewPipelineService(cfg *config.Config) *PipelineService {
	return &PipelineService{
		cfg:  cfg,
		repo: repository.NewPipelineRepository(),
	}
}

// CreatePipeline creates a new pipeline
func (s *PipelineService) CreatePipeline(ctx context.Context, req *model.PipelineCreateRequest, userID uint) (*model.Pipeline, error) {
	if s.repo.ExistsByName(req.Name) {
		return nil, nil // Already exists
	}

	pipeline := &model.Pipeline{
		ID:              uuid.New().String(),
		Name:            req.Name,
		BaseType:        req.BaseType,
		Version:         req.Version,
		Description:     req.Description,
		BEDFile:         req.BEDFile,
		ReferenceGenome: req.ReferenceGenome,
		CNVBaseline:     req.CNVBaseline,
		Status:          model.PipelineStatusActive,
		CreatedBy:       userID,
	}

	if err := s.repo.Create(pipeline); err != nil {
		return nil, err
	}

	return pipeline, nil
}

// GetPipeline gets a pipeline by UUID
func (s *PipelineService) GetPipeline(ctx context.Context, id string) (*model.Pipeline, error) {
	return s.repo.FindByUUID(id)
}

// ListPipelines lists pipelines with pagination
func (s *PipelineService) ListPipelines(ctx context.Context, query *model.PipelineListQuery) (*model.PipelineListResponse, error) {
	pipelines, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.PipelineResponse, len(pipelines))
	for i, p := range pipelines {
		items[i] = p.ToResponse()
	}

	return &model.PipelineListResponse{
		Total: int(total),
		Items: items,
	}, nil
}

// UpdatePipeline updates a pipeline
func (s *PipelineService) UpdatePipeline(ctx context.Context, id string, req *model.PipelineUpdateRequest) (*model.Pipeline, error) {
	pipeline, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		pipeline.Name = req.Name
	}
	if req.BaseType != "" {
		pipeline.BaseType = req.BaseType
	}
	if req.Version != "" {
		pipeline.Version = req.Version
	}
	if req.Description != "" {
		pipeline.Description = req.Description
	}
	if req.BEDFile != "" {
		pipeline.BEDFile = req.BEDFile
	}
	if req.ReferenceGenome != "" {
		pipeline.ReferenceGenome = req.ReferenceGenome
	}
	if req.CNVBaseline != "" {
		pipeline.CNVBaseline = req.CNVBaseline
	}
	if req.Status != "" {
		pipeline.Status = req.Status
	}

	if err := s.repo.Update(pipeline); err != nil {
		return nil, err
	}

	return pipeline, nil
}

// DeletePipeline deletes a pipeline
func (s *PipelineService) DeletePipeline(ctx context.Context, id string) error {
	pipeline, err := s.repo.FindByUUID(id)
	if err != nil {
		return err
	}
	return s.repo.Delete(pipeline.ID)
}
