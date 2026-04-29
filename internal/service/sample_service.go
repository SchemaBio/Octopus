package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
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
	if s.repo.ExistsByInternalID(req.InternalID) {
		return nil, nil // Already exists
	}

	sample := &model.Sample{
		UUID:              uuid.New().String(),
		InternalID:        req.InternalID,
		Gender:            req.Gender,
		Age:               req.Age,
		SampleType:        req.SampleType,
		Batch:             req.Batch,
		ClinicalDiagnosis: req.ClinicalDiagnosis,
		Remark:            req.Remark,
		Status:            model.SampleStatusPending,
		CreatedBy:         userID,
	}

	if sample.Gender == "" {
		sample.Gender = model.SampleGenderUnknown
	}
	if sample.SampleType == "" {
		sample.SampleType = model.SampleTypeOther
	}

	// Set HPO terms
	if req.HPOTerms != nil {
		sample.SetHPOTerms(req.HPOTerms)
	}

	// Set matched pair from R1/R2 paths
	if req.R1Path != "" || req.R2Path != "" {
		sample.SetMatchedPair(&model.MatchedPair{
			R1Path: req.R1Path,
			R2Path: req.R2Path,
		})
	}

	if err := s.repo.Create(sample); err != nil {
		return nil, err
	}

	return sample, nil
}

// GetSample gets a sample by UUID
func (s *SampleService) GetSample(ctx context.Context, id string) (*model.Sample, error) {
	return s.repo.FindByUUID(id)
}

// ListSamples lists samples with pagination and filters
func (s *SampleService) ListSamples(ctx context.Context, query *model.SampleListQuery) (*model.SampleListResponse, error) {
	samples, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.SampleResponse, len(samples))
	for i, sample := range samples {
		items[i] = model.SampleToResponse(&sample)
	}

	return &model.SampleListResponse{
		Total: int(total),
		Items: items,
	}, nil
}

// UpdateSample updates a sample
func (s *SampleService) UpdateSample(ctx context.Context, id string, req *model.SampleUpdateRequest) (*model.Sample, error) {
	sample, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, err
	}

	if req.InternalID != "" {
		sample.InternalID = req.InternalID
	}
	if req.Gender != "" {
		sample.Gender = req.Gender
	}
	if req.Age != nil {
		sample.Age = req.Age
	}
	if req.SampleType != "" {
		sample.SampleType = req.SampleType
	}
	if req.Batch != "" {
		sample.Batch = req.Batch
	}
	if req.ClinicalDiagnosis != "" {
		sample.ClinicalDiagnosis = req.ClinicalDiagnosis
	}
	if req.HPOTerms != nil {
		sample.SetHPOTerms(req.HPOTerms)
	}
	if req.R1Path != "" || req.R2Path != "" {
		sample.SetMatchedPair(&model.MatchedPair{
			R1Path: req.R1Path,
			R2Path: req.R2Path,
		})
	}
	if req.Remark != "" {
		sample.Remark = req.Remark
	}
	if req.Status != "" {
		sample.Status = req.Status
	}

	if err := s.repo.Update(sample); err != nil {
		return nil, err
	}

	return sample, nil
}

// DeleteSample deletes a sample
func (s *SampleService) DeleteSample(ctx context.Context, id string) error {
	sample, err := s.repo.FindByUUID(id)
	if err != nil {
		return err
	}
	return s.repo.Delete(sample.ID)
}

// SampleToResponse converts sample to response
func (s *SampleService) SampleToResponse(sample *model.Sample) model.SampleResponse {
	return model.SampleToResponse(sample)
}

// SampleToDetailResponse converts sample to detail response
func (s *SampleService) SampleToDetailResponse(sample *model.Sample) model.SampleDetailResponse {
	return model.SampleDetailResponse{
		ID:                sample.UUID,
		InternalID:        sample.InternalID,
		Gender:            sample.Gender,
		Age:               sample.Age,
		SampleType:        sample.SampleType,
		Batch:             sample.Batch,
		MatchedPair:       sample.GetMatchedPair(),
		Remark:            sample.Remark,
		ClinicalDiagnosis: parseClinicalDiagnosis(sample.ClinicalDiagnosis),
		SubmissionInfo:    sample.GetSubmissionInfo(),
		ProjectInfo:       sample.GetProjectInfo(),
		FamilyHistory:     sample.GetFamilyHistory(),
		AnalysisTasks:     []model.AnalysisTaskBrief{},
		CreatedAt:         sample.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         sample.UpdatedAt.Format(time.RFC3339),
	}
}

func parseClinicalDiagnosis(s string) model.ClinicalDiagnosisInfo {
	if s == "" {
		return model.ClinicalDiagnosisInfo{}
	}
	var info model.ClinicalDiagnosisInfo
	// Try parsing as structured JSON first
	if err := json.Unmarshal([]byte(s), &info); err == nil {
		return info
	}
	// Fallback: treat as plain string
	return model.ClinicalDiagnosisInfo{
		MainDiagnosis: s,
	}
}
