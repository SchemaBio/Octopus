package service

import (
	"context"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
)

// SampleService handles sample business logic
type SampleService struct {
	cfg  *config.Config
	repo *repository.SampleRepository
}

// NewSampleService creates a new sample service
func NewSampleService(cfg *config.Config) *SampleService {
	return &SampleService{
		cfg:  cfg,
		repo: repository.NewSampleRepository(),
	}
}

// CreateSample creates a new sample
func (s *SampleService) CreateSample(ctx context.Context, req *model.SampleCreateRequest, userID uint) (*model.Sample, error) {
	// Check if sample_id exists
	if s.repo.ExistsBySampleID(req.SampleID) {
		return nil, nil // Already exists
	}

	sample := &model.Sample{
		SampleID:   req.SampleID,
		SampleName: req.SampleName,
		SampleType: req.SampleType,
		Source:     req.Source,
		ProjectID:  req.ProjectID,
		Status:     model.SampleStatusPending,
		Metadata:   req.Metadata,
		CreatedBy:  userID,
	}

	if err := s.repo.Create(sample); err != nil {
		return nil, err
	}

	return sample, nil
}

// GetSample gets a sample by ID
func (s *SampleService) GetSample(ctx context.Context, id uint) (*model.Sample, error) {
	return s.repo.FindByID(id)
}

// GetSampleBySampleID gets a sample by sample_id (business identifier)
func (s *SampleService) GetSampleBySampleID(ctx context.Context, sampleID string) (*model.Sample, error) {
	return s.repo.FindBySampleID(sampleID)
}

// ListSamples lists samples with pagination and filters
func (s *SampleService) ListSamples(ctx context.Context, query *model.SampleListQuery) (*model.SampleListResponse, error) {
	samples, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.SampleResponse, len(samples))
	for i, sample := range samples {
		items[i] = model.SampleResponse{
			ID:         sample.ID,
			SampleID:   sample.SampleID,
			SampleName: sample.SampleName,
			SampleType: sample.SampleType,
			Source:     sample.Source,
			ProjectID:  sample.ProjectID,
			Status:     sample.Status,
			Metadata:   sample.Metadata,
			CreatedBy:  sample.CreatedBy,
			CreatedAt:  sample.CreatedAt,
			UpdatedAt:  sample.UpdatedAt,
		}
	}

	return &model.SampleListResponse{
		Total: int(total),
		Items: items,
	}, nil
}

// UpdateSample updates a sample
func (s *SampleService) UpdateSample(ctx context.Context, id uint, req *model.SampleUpdateRequest) (*model.Sample, error) {
	sample, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	if req.SampleName != "" {
		sample.SampleName = req.SampleName
	}
	if req.SampleType != "" {
		sample.SampleType = req.SampleType
	}
	if req.Source != "" {
		sample.Source = req.Source
	}
	if req.ProjectID > 0 {
		sample.ProjectID = req.ProjectID
	}
	if req.Status != "" {
		sample.Status = req.Status
	}
	if req.Metadata != "" {
		sample.Metadata = req.Metadata
	}

	if err := s.repo.Update(sample); err != nil {
		return nil, err
	}

	return sample, nil
}

// DeleteSample deletes a sample
func (s *SampleService) DeleteSample(ctx context.Context, id uint) error {
	return s.repo.Delete(id)
}

// AssignProject assigns samples to a project
func (s *SampleService) AssignProject(ctx context.Context, sampleIDs []uint, projectID uint) error {
	return s.repo.AssignProject(sampleIDs, projectID)
}

// UpdateStatus updates sample status
func (s *SampleService) UpdateStatus(ctx context.Context, id uint, status model.SampleStatus) error {
	return s.repo.UpdateStatus(id, status)
}

// GetSamplesByProject gets all samples for a project
func (s *SampleService) GetSamplesByProject(ctx context.Context, projectID uint) ([]model.Sample, error) {
	return s.repo.FindByProjectID(projectID)
}

// SampleToResponse converts sample to response format
func (s *SampleService) SampleToResponse(sample *model.Sample) model.SampleResponse {
	return model.SampleResponse{
		ID:         sample.ID,
		SampleID:   sample.SampleID,
		SampleName: sample.SampleName,
		SampleType: sample.SampleType,
		Source:     sample.Source,
		ProjectID:  sample.ProjectID,
		Status:     sample.Status,
		Metadata:   sample.Metadata,
		CreatedBy:  sample.CreatedBy,
		CreatedAt:  sample.CreatedAt,
		UpdatedAt:  sample.UpdatedAt,
	}
}