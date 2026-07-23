package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SampleService handles sample business logic
type SampleService struct {
	cfg            *config.Config
	repo           *repository.SampleRepository
	uploadJobRepo  *repository.UploadJobRepository
	uploadFileRepo *repository.UploadFileRepository
	assetRepo      *repository.DataAssetRepository
}

// NewSampleService creates a new sample service
func NewSampleService(cfg *config.Config) *SampleService {
	return &SampleService{
		cfg:            cfg,
		repo:           repository.NewSampleRepository(),
		uploadJobRepo:  repository.NewUploadJobRepository(),
		uploadFileRepo: repository.NewUploadFileRepository(),
		assetRepo:      repository.NewDataAssetRepository(),
	}
}

// CreateSample creates a new sample
func (s *SampleService) CreateSample(ctx context.Context, req *model.SampleCreateRequest, actor model.OverlayActor) (*model.Sample, error) {
	if s.repo.ExistsByInternalIDInScope(req.InternalID, actor.OrgID, actor.UserID, 0) {
		return nil, nil // Already exists
	}
	if err := validateActorFileReference(s.cfg, actor, "r1_path", req.R1Path); err != nil {
		return nil, err
	}
	if err := validateActorFileReference(s.cfg, actor, "r2_path", req.R2Path); err != nil {
		return nil, err
	}

	sample := &model.Sample{
		UUID:             uuid.New().String(),
		InternalID:       req.InternalID,
		ExternalOrgID:    actor.OrgID,
		Gender:           req.Gender,
		Age:              req.Age,
		SampleType:       req.SampleType,
		Batch:            req.Batch,
		Remark:           req.Remark,
		Status:           model.SampleStatusPending,
		CreatedBy:        actor.UserID,
		MatchStatus:      model.SampleMatchUnmatched,
		AutoMatchEnabled: true,
	}

	if sample.Gender == "" {
		sample.Gender = model.SampleGenderUnknown
	}
	if sample.SampleType == "" {
		sample.SampleType = model.SampleTypeOther
	}
	sample.SetClinicalDiagnosis(req.ClinicalDiagnosis)
	sample.SetMatchedPair(nil)
	sample.SetAutoMatchedPair(nil)
	sample.SetSubmissionInfo(model.SubmissionInfo{})
	sample.SetProjectInfo(model.ProjectInfo{})
	sample.SetFamilyHistory(model.FamilyHistoryInfo{})

	// Set HPO terms
	sample.SetHPOTerms(req.HPOTerms)

	// Set matched pair from R1/R2 paths
	if req.R1Path != "" || req.R2Path != "" {
		sample.SetMatchedPair(&model.MatchedPair{
			R1Path: req.R1Path,
			R2Path: req.R2Path,
		})
		sample.MatchStatus = model.SampleMatchMatched
		sample.MatchMode = model.SampleMatchModeManual
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

// LinkDataAssets creates an explicit manual R1/R2 relationship. Automatic
// matching is never allowed to overwrite this row.
func (s *SampleService) LinkDataAssets(ctx context.Context, id string, req *model.SampleDataLinkRequest, actor model.OverlayActor) (*model.Sample, error) {
	sample, err := s.repo.FindScopedByUUID(id, actor)
	if err != nil {
		return nil, fmt.Errorf("sample not found")
	}
	read1, err := s.assetRepo.FindScopedByUUID(req.Read1AssetID, actor)
	if err != nil {
		return nil, fmt.Errorf("Read1 data asset not found")
	}
	read2, err := s.assetRepo.FindScopedByUUID(req.Read2AssetID, actor)
	if err != nil {
		return nil, fmt.Errorf("Read2 data asset not found")
	}
	if read1.Status != model.FileStatusCompleted || read2.Status != model.FileStatusCompleted {
		return nil, fmt.Errorf("data assets must be ready before matching")
	}
	if read1.ReadType != model.ReadTypeRead1 || read2.ReadType != model.ReadTypeRead2 {
		return nil, fmt.Errorf("Read1 and Read2 assets have incompatible read types")
	}

	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		var locked model.Sample
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, sample.ID).Error; err != nil {
			return err
		}
		now := time.Now()
		link := model.SampleDataLink{
			SampleID: locked.ID, ExternalOrgID: locked.ExternalOrgID,
			Read1AssetID: read1.ID, Read2AssetID: read2.ID,
			MatchMode: model.SampleMatchModeManual, MatchRule: "manual_selection",
			MatchedBy: actor.UserID, MatchedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "sample_id"}, {Name: "match_mode"}},
			DoUpdates: clause.AssignmentColumns([]string{"external_org_id", "read1_asset_id", "read2_asset_id", "match_mode", "match_rule", "matched_by", "matched_at", "updated_at"}),
		}).Create(&link).Error; err != nil {
			return err
		}
		locked.SetMatchedPair(&model.MatchedPair{R1Path: read1.StorageKey, R2Path: read2.StorageKey})
		locked.MatchStatus = model.SampleMatchMatched
		locked.MatchMode = model.SampleMatchModeManual
		return tx.Save(&locked).Error
	})
	if err != nil {
		return nil, err
	}
	return s.repo.FindScopedByUUID(id, actor)
}

func (s *SampleService) GetSampleScoped(ctx context.Context, id string, actor model.OverlayActor) (*model.Sample, error) {
	return s.repo.FindScopedByUUID(id, actor)
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
	sample, err := s.repo.FindScopedByUUID(id, actor)
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
		if s.repo.ExistsByInternalIDInScope(req.InternalID, sample.ExternalOrgID, sample.CreatedBy, sample.ID) {
			return nil, fmt.Errorf("internal_id already exists")
		}
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
		sample.SetClinicalDiagnosis(req.ClinicalDiagnosis)
	}
	if req.HPOTerms != nil {
		sample.SetHPOTerms(req.HPOTerms)
	}
	if req.R1Path != "" || req.R2Path != "" {
		sample.SetMatchedPair(&model.MatchedPair{
			R1Path: req.R1Path,
			R2Path: req.R2Path,
		})
		sample.MatchStatus = model.SampleMatchMatched
		sample.MatchMode = model.SampleMatchModeManual
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

// ClearMatchedPair removes only the explicit/manual selection. If an automatic
// pair exists, it immediately becomes the effective match again.
func (s *SampleService) ClearMatchedPair(ctx context.Context, id string, actor model.OverlayActor) (*model.Sample, error) {
	sample, err := s.repo.FindScopedByUUID(id, actor)
	if err != nil {
		return nil, err
	}
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		var locked model.Sample
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, sample.ID).Error; err != nil {
			return err
		}
		locked.SetMatchedPair(nil)
		if err := tx.Where("sample_id = ? AND match_mode = ?", locked.ID, model.SampleMatchModeManual).Delete(&model.SampleDataLink{}).Error; err != nil {
			return err
		}
		if locked.GetAutoMatchedPair() != nil {
			locked.MatchStatus = model.SampleMatchMatched
			locked.MatchMode = model.SampleMatchModeAutomatic
		} else {
			locked.MatchStatus = model.SampleMatchUnmatched
			locked.MatchMode = ""
		}
		return tx.Save(&locked).Error
	})
	if err != nil {
		return nil, err
	}
	return s.repo.FindScopedByUUID(id, actor)
}

// MatchFromUploadJob binds a completed paired FASTQ upload job to a sample without exposing storage paths to the browser.
func (s *SampleService) MatchFromUploadJob(ctx context.Context, id string, uploadJobID string, actor model.OverlayActor) (*model.Sample, error) {
	_, err := s.repo.FindScopedByUUID(id, actor)
	if err != nil {
		return nil, err
	}
	uploadJob, err := s.uploadJobRepo.FindByUUID(uploadJobID)
	if err != nil {
		return nil, fmt.Errorf("upload job not found")
	}
	if actor.Role != string(model.SystemRoleSuperAdmin) && ((actor.OrgID != "" && uploadJob.ExternalOrgID != actor.OrgID) || (actor.OrgID == "" && uploadJob.UserID != actor.UserID)) {
		return nil, fmt.Errorf("upload job not found")
	}
	if uploadJob.Status != model.UploadJobStatusCompleted {
		return nil, fmt.Errorf("upload job status is %s", uploadJob.Status)
	}

	files, err := s.uploadFileRepo.FindByJobID(uploadJob.ID)
	if err != nil {
		return nil, err
	}
	var r1Path, r2Path, r1AssetUUID, r2AssetUUID string
	for _, file := range files {
		if file.Status != model.FileStatusCompleted {
			continue
		}
		switch file.ReadType {
		case model.ReadTypeRead1:
			r1Path = file.StorageKey
			if asset, assetErr := s.assetRepo.FindByUploadFileID(file.ID); assetErr == nil {
				r1AssetUUID = asset.UUID
			}
		case model.ReadTypeRead2:
			r2Path = file.StorageKey
			if asset, assetErr := s.assetRepo.FindByUploadFileID(file.ID); assetErr == nil {
				r2AssetUUID = asset.UUID
			}
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
	if r1AssetUUID == "" || r2AssetUUID == "" {
		return nil, fmt.Errorf("upload job data assets not found")
	}
	return s.LinkDataAssets(ctx, id, &model.SampleDataLinkRequest{
		Read1AssetID: r1AssetUUID,
		Read2AssetID: r2AssetUUID,
	}, actor)
}

// DeleteSample deletes a sample
func (s *SampleService) DeleteSample(ctx context.Context, id string, actor model.OverlayActor) error {
	sample, err := s.repo.FindScopedByUUID(id, actor)
	if err != nil {
		return err
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("sample_id = ?", sample.ID).Delete(&model.SampleDataLink{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Sample{}, sample.ID).Error
	})
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
		MatchedPair:       sample.PublicMatchedPair(),
		MatchStatus:       sample.MatchStatus,
		MatchMode:         sample.MatchMode,
		AutoMatchEnabled:  sample.AutoMatchEnabled,
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
