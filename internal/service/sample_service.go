package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
)

// SampleService handles sample business logic
type SampleService struct {
	cfg            *config.Config
	repo           *repository.SampleRepository
	uploadJobRepo  *repository.UploadJobRepository
	uploadFileRepo *repository.UploadFileRepository
}

// NewSampleService creates a new sample service
func NewSampleService(cfg *config.Config) *SampleService {
	return &SampleService{
		cfg:            cfg,
		repo:           repository.NewSampleRepository(),
		uploadJobRepo:  repository.NewUploadJobRepository(),
		uploadFileRepo: repository.NewUploadFileRepository(),
	}
}

// CreateSample creates a new sample
func (s *SampleService) CreateSample(ctx context.Context, req *model.SampleCreateRequest, actor model.OverlayActor) (*model.Sample, error) {
	if s.repo.ExistsByInternalID(req.InternalID) {
		return nil, nil // Already exists
	}
	if err := validateActorFileReference(s.cfg, actor, "r1_path", req.R1Path); err != nil {
		return nil, err
	}
	if err := validateActorFileReference(s.cfg, actor, "r2_path", req.R2Path); err != nil {
		return nil, err
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
		CreatedBy:         actor.UserID,
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
		Total: total,
		Items: items,
	}, nil
}

// UpdateSample updates a sample
func (s *SampleService) UpdateSample(ctx context.Context, id string, req *model.SampleUpdateRequest, actor model.OverlayActor) (*model.Sample, error) {
	sample, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, err
	}
	if err := validateActorFileReference(s.cfg, actor, "r1_path", req.R1Path); err != nil {
		return nil, err
	}
	if err := validateActorFileReference(s.cfg, actor, "r2_path", req.R2Path); err != nil {
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

// ClearMatchedPair removes the FASTQ matched pair from a sample.
func (s *SampleService) ClearMatchedPair(ctx context.Context, id string) (*model.Sample, error) {
	sample, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, err
	}
	sample.SetMatchedPair(nil)
	if err := s.repo.Update(sample); err != nil {
		return nil, err
	}
	return sample, nil
}

// MatchFromUploadJob binds a completed paired FASTQ upload job to a sample without exposing storage paths to the browser.
func (s *SampleService) MatchFromUploadJob(ctx context.Context, id string, uploadJobID string, actor model.OverlayActor) (*model.Sample, error) {
	sample, err := s.repo.FindByUUID(id)
	if err != nil {
		return nil, err
	}
	uploadJob, err := s.uploadJobRepo.FindByUUID(uploadJobID)
	if err != nil {
		return nil, fmt.Errorf("upload job not found")
	}
	if !actorCanUseOwnedResource(actor, uploadJob.UserID) {
		return nil, fmt.Errorf("upload job not found")
	}
	if uploadJob.Status != model.UploadJobStatusCompleted {
		return nil, fmt.Errorf("upload job status is %s", uploadJob.Status)
	}

	files, err := s.uploadFileRepo.FindByJobID(uploadJob.ID)
	if err != nil {
		return nil, err
	}
	var r1Path, r2Path string
	for _, file := range files {
		if file.Status != model.FileStatusCompleted {
			continue
		}
		switch file.ReadType {
		case model.ReadTypeRead1:
			r1Path = file.StorageKey
		case model.ReadTypeRead2:
			r2Path = file.StorageKey
		}
	}
	if r1Path == "" || r2Path == "" {
		return nil, fmt.Errorf("upload job must contain completed read1 and read2 files")
	}
	if err := validateActorFileReference(s.cfg, actor, "upload job read1", r1Path); err != nil {
		return nil, err
	}
	if err := validateActorFileReference(s.cfg, actor, "upload job read2", r2Path); err != nil {
		return nil, err
	}

	sample.SetMatchedPair(&model.MatchedPair{R1Path: r1Path, R2Path: r2Path})
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
