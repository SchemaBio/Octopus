package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UploadService struct {
	cfg       *config.Config
	jobRepo   *repository.UploadJobRepository
	fileRepo  *repository.UploadFileRepository
	assetRepo *repository.DataAssetRepository
	userRepo  *repository.UserRepository
}

var safeUploadFilename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+=-]{0,254}$`)

const maxBEDUploadBytes int64 = 20 << 20

type preparedUploadMetadata struct {
	file  *model.UploadFile
	asset *model.DataAsset
}

func normalizeReferenceGenome(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "GRCH37", "HG19":
		return model.ReferenceGenomeGRCh37, nil
	case "GRCH38", "HG38":
		return model.ReferenceGenomeGRCh38, nil
	default:
		return "", fmt.Errorf("reference_genome must be GRCh37 or GRCh38")
	}
}

func safeStorageSegment(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(value, "_")
}

func validateUploadFilename(filename string) error {
	if filename == "" {
		return fmt.Errorf("filename is required")
	}
	if filename != filepath.Base(filename) || strings.Contains(filename, `\`) {
		return fmt.Errorf("filename must not contain path separators")
	}
	if filename == "." || filename == ".." || !safeUploadFilename.MatchString(filename) {
		return fmt.Errorf("filename contains unsupported characters")
	}
	return nil
}

func NewUploadService(cfg *config.Config) *UploadService {
	return &UploadService{
		cfg:       cfg,
		jobRepo:   repository.NewUploadJobRepository(),
		fileRepo:  repository.NewUploadFileRepository(),
		assetRepo: repository.NewDataAssetRepository(),
		userRepo:  repository.NewUserRepository(),
	}
}

func (s *UploadService) Config() *config.Config {
	return s.cfg
}

func (s *UploadService) getUserStorageFolder(ctx context.Context, userID uint) (string, error) {
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return "", fmt.Errorf("user not found: %w", err)
	}

	if user.StorageFolder == "" {
		user.StorageFolder = uuid.New().String()
		if err := s.userRepo.Update(user); err != nil {
			return "", fmt.Errorf("failed to update user storage folder: %w", err)
		}
	}

	return user.StorageFolder, nil
}

func (s *UploadService) CreateJob(ctx context.Context, userID uint, orgID string, req *model.UploadJobCreateRequest) (*model.UploadJob, []*model.UploadFile, []string, error) {
	if len(req.Files) == 0 {
		return nil, nil, nil, fmt.Errorf("at least one file is required")
	}
	if err := validateUploadPolicyAcknowledgement(s.cfg.Storage.RetentionDays, req.UploadPolicyAcknowledged); err != nil {
		return nil, nil, nil, err
	}
	req.InternalID = strings.TrimSpace(req.InternalID)

	if req.FileType == model.UploadFileTypeBed {
		genome, err := normalizeReferenceGenome(req.ReferenceGenome)
		if err != nil {
			return nil, nil, nil, err
		}
		req.ReferenceGenome = genome
		if len(req.Files) != 1 || req.Files[0].ReadType != model.ReadTypeBed {
			return nil, nil, nil, fmt.Errorf("a BED upload must contain exactly one bed file")
		}
	} else {
		req.ReferenceGenome = ""
	}

	var requestedBytes int64
	for _, f := range req.Files {
		if err := validateUploadFilename(f.FileName); err != nil {
			return nil, nil, nil, err
		}
		if f.FileSize <= 0 {
			return nil, nil, nil, fmt.Errorf("file size must be positive")
		}
		if requestedBytes > math.MaxInt64-f.FileSize {
			return nil, nil, nil, fmt.Errorf("total upload size is too large")
		}
		requestedBytes += f.FileSize
		if err := validateSaaSUploadFileSize(s.cfg.Storage.RetentionDays, f.FileName, f.FileSize); err != nil {
			return nil, nil, nil, err
		}
		if s.cfg.Storage.MaxSizeMB > 0 && f.FileSize > int64(s.cfg.Storage.MaxSizeMB)*1024*1024 {
			return nil, nil, nil, fmt.Errorf("file %s exceeds maximum size of %d MB", f.FileName, s.cfg.Storage.MaxSizeMB)
		}
		if req.FileType == model.UploadFileTypeBed {
			lowerName := strings.ToLower(f.FileName)
			if !strings.HasSuffix(lowerName, ".bed") && !strings.HasSuffix(lowerName, ".bed.gz") {
				return nil, nil, nil, fmt.Errorf("BED file must use .bed or .bed.gz extension")
			}
			if f.FileSize > maxBEDUploadBytes {
				return nil, nil, nil, fmt.Errorf("BED file exceeds maximum size of 20 MB")
			}
		}
	}

	provider := model.UploadProvider(s.cfg.Storage.Provider)
	if _, err := tenantUploadCategory(req.FileType, req.ReferenceGenome); err != nil {
		return nil, nil, nil, err
	}
	if provider == model.UploadProviderS3 && strings.TrimSpace(orgID) != "" {
		var err error
		orgID, err = normalizeTenantID(orgID)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	var objectStore *s3Storage
	if provider == model.UploadProviderS3 {
		var err error
		objectStore, err = newS3Storage(ctx, s.cfg.Storage)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	jobUUID := uuid.New().String()
	job := &model.UploadJob{
		UUID:            jobUUID,
		UserID:          userID,
		ExternalOrgID:   orgID,
		SampleID:        req.SampleID,
		InternalID:      req.InternalID,
		Name:            req.Name,
		FileType:        req.FileType,
		ReferenceGenome: req.ReferenceGenome,
		Provider:        provider,
		Status:          model.UploadJobStatusPending,
	}
	if s.cfg.Storage.RetentionDays > 0 {
		acknowledgedAt := time.Now().UTC()
		job.UploadPolicyVersion = model.DataUploadPolicyVersion
		if req.FileType == model.UploadFileTypeBed {
			job.UploadPolicyVersion = model.BEDUploadPolicyVersion
		}
		job.UploadPolicyAcknowledgedAt = &acknowledgedAt
	}

	storageFolder := ""
	if provider != model.UploadProviderS3 || orgID == "" {
		var err error
		storageFolder, err = s.getUserStorageFolder(ctx, userID)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	prepared := make([]preparedUploadMetadata, 0, len(req.Files))
	presignedURLs := make([]string, 0, len(req.Files))
	assetTime := time.Now()
	for _, f := range req.Files {
		fileUUID := uuid.New().String()
		storageKey := ""
		if provider == model.UploadProviderS3 && orgID != "" {
			var err error
			storageKey, err = tenantUploadObjectKey(orgID, jobUUID, req.FileType, req.ReferenceGenome, f.FileName)
			if err != nil {
				return nil, nil, nil, err
			}
		} else {
			storageKey = s.buildStorageKey(provider, orgID, storageFolder, jobUUID, req.FileType, req.ReferenceGenome, f.FileName)
		}

		uploadFile := &model.UploadFile{
			UUID:       fileUUID,
			JobUUID:    jobUUID,
			FileName:   f.FileName,
			StorageKey: storageKey,
			FileSize:   f.FileSize,
			ReadType:   f.ReadType,
			Status:     model.FileStatusPending,
		}

		expiresAt := dataAssetExpiry(s.cfg.Storage.RetentionDays, f.ReadType, assetTime)
		asset := &model.DataAsset{
			UUID: fileUUID, ExternalOrgID: orgID, CreatedBy: userID,
			Provider: provider, StorageKey: storageKey,
			FileName: f.FileName, InternalID: req.InternalID, FileSize: f.FileSize, ReadType: f.ReadType,
			ReferenceGenome: req.ReferenceGenome,
			Status:          model.FileStatusPending, Source: model.DataAssetSourceUpload, ExpiresAt: expiresAt,
		}
		if objectStore != nil {
			url, err := objectStore.presignUpload(ctx, storageKey)
			if err != nil {
				return nil, nil, nil, err
			}
			presignedURLs = append(presignedURLs, url)
		} else {
			presignedURLs = append(presignedURLs, "")
		}
		prepared = append(prepared, preparedUploadMetadata{file: uploadFile, asset: asset})
	}
	db := database.GetDB()
	if db == nil {
		return nil, nil, nil, fmt.Errorf("database is not initialized")
	}
	if err := persistUploadMetadata(db.WithContext(ctx), job, prepared, requestedBytes, req.StorageQuotaBytes); err != nil {
		return nil, nil, nil, err
	}
	files := make([]*model.UploadFile, len(prepared))
	for i := range prepared {
		files[i] = prepared[i].file
	}

	return job, files, presignedURLs, nil
}

func persistUploadMetadata(db *gorm.DB, job *model.UploadJob, prepared []preparedUploadMetadata, requestedBytes, quotaBytes int64) error {
	if db == nil {
		return fmt.Errorf("database is not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if quotaBytes > 0 && job.ExternalOrgID != "" {
			lockKey := "octopus:storage-quota:" + job.ExternalOrgID
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
				return fmt.Errorf("failed to lock organization storage quota: %w", err)
			}
			usedBytes, err := sumReservedOrganizationBytes(tx, job.ExternalOrgID)
			if err != nil {
				return fmt.Errorf("failed to check organization storage quota: %w", err)
			}
			if err := validateStorageQuota(usedBytes, requestedBytes, quotaBytes); err != nil {
				return err
			}
		}
		if err := tx.Create(job).Error; err != nil {
			return fmt.Errorf("failed to create upload job: %w", err)
		}
		for _, item := range prepared {
			item.file.JobID = job.ID
			if err := tx.Create(item.file).Error; err != nil {
				return fmt.Errorf("failed to create upload file record: %w", err)
			}
			item.asset.UploadFileID = &item.file.ID
			if err := tx.Create(item.asset).Error; err != nil {
				return fmt.Errorf("failed to register data asset: %w", err)
			}
		}
		return nil
	})
}

func sumReservedOrganizationBytes(tx *gorm.DB, orgID string) (int64, error) {
	var sum int64
	err := tx.Model(&model.DataAsset{}).
		Where("external_org_id = ? AND status <> ?", orgID, model.FileStatusDeleted).
		Select("COALESCE(SUM(file_size), 0)").
		Scan(&sum).Error
	return sum, err
}

func validateStorageQuota(usedBytes, requestedBytes, quotaBytes int64) error {
	if quotaBytes <= 0 {
		return nil
	}
	if usedBytes < 0 || requestedBytes < 0 || usedBytes > quotaBytes || requestedBytes > quotaBytes-usedBytes {
		return fmt.Errorf("organization storage quota exceeded")
	}
	return nil
}

func dataAssetExpiry(retentionDays int, readType model.ReadType, now time.Time) *time.Time {
	if retentionDays <= 0 || readType == model.ReadTypeBed {
		return nil
	}
	value := now.AddDate(0, 0, retentionDays)
	return &value
}

func validateUploadPolicyAcknowledgement(retentionDays int, acknowledged bool) error {
	if retentionDays > 0 && !acknowledged {
		return fmt.Errorf("upload policy acknowledgement is required")
	}
	return nil
}

func validateSaaSUploadFileSize(retentionDays int, fileName string, fileSize int64) error {
	if retentionDays > 0 && fileSize > model.SaaSMaxUploadFileBytes {
		return fmt.Errorf("file %s exceeds SaaS maximum size of 20 GB", fileName)
	}
	return nil
}

func (s *UploadService) buildStorageKey(provider model.UploadProvider, orgID, storageFolder, jobUUID string, fileType model.UploadFileType, referenceGenome, fileName string) string {
	if fileType == model.UploadFileTypeBed {
		scope := safeStorageSegment(orgID, "user-"+storageFolder)
		if provider == model.UploadProviderS3 {
			return path.Join("organizations", scope, "bed", referenceGenome, jobUUID, fileName)
		}
		return filepath.Join("bed", referenceGenome, scope, jobUUID, fileName)
	}
	if provider == model.UploadProviderS3 {
		return path.Join("uploads", storageFolder, jobUUID, fileName)
	}
	return filepath.Join(storageFolder, jobUUID, fileName)
}

func (s *UploadService) SaveLocalFile(ctx context.Context, actor model.OverlayActor, fileUUID string, reader io.Reader, fileSize int64) (*model.UploadFile, error) {
	existingFile, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, fmt.Errorf("upload file record not found: %w", err)
	}

	job, err := s.jobRepo.FindByUUID(existingFile.JobUUID)
	if err != nil {
		return nil, fmt.Errorf("upload job not found: %w", err)
	}
	if !uploadJobAccessAllowed(job, actor) {
		return nil, fmt.Errorf("upload file not found")
	}
	if job.Provider != model.UploadProviderLocal {
		return nil, fmt.Errorf("upload job does not use local storage")
	}
	if existingFile.Status != model.FileStatusPending && existingFile.Status != model.FileStatusFailed {
		return nil, fmt.Errorf("upload file is not writable")
	}
	if err := validateUploadFilename(existingFile.FileName); err != nil {
		return nil, err
	}
	if fileSize <= 0 || (existingFile.FileSize > 0 && fileSize != existingFile.FileSize) {
		return nil, fmt.Errorf("uploaded file size mismatch: expected %d, got %d", existingFile.FileSize, fileSize)
	}

	storagePath := existingFile.StorageKey
	if !filepath.IsAbs(storagePath) {
		storagePath = filepath.Join(s.cfg.Storage.LocalDir, storagePath)
	}
	if err := ensurePathInsideBase(s.cfg.Storage.LocalDir, storagePath); err != nil {
		return nil, err
	}
	dir := filepath.Dir(storagePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	filePath := storagePath
	maxSize := existingFile.FileSize
	if s.cfg.Storage.MaxSizeMB > 0 {
		configuredMax := int64(s.cfg.Storage.MaxSizeMB) * 1024 * 1024
		if maxSize <= 0 || configuredMax < maxSize {
			maxSize = configuredMax
		}
	}
	if job.FileType == model.UploadFileTypeBed && (maxSize <= 0 || maxBEDUploadBytes < maxSize) {
		maxSize = maxBEDUploadBytes
	}
	written, err := writeLocalUploadFile(filePath, reader, existingFile.FileSize, maxSize)
	if err != nil {
		return nil, err
	}
	db := database.GetDB()
	if db == nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("database is not initialized")
	}
	allComplete, err := completeUploadMetadata(db.WithContext(ctx), existingFile, job, written, filePath)
	if err != nil {
		_ = os.Remove(filePath)
		return nil, err
	}
	if allComplete {
		NewSampleMatcher().run(context.Background())
	}

	return existingFile, nil
}

func writeLocalUploadFile(filePath string, reader io.Reader, expectedSize, maxSize int64) (int64, error) {
	temp, err := os.CreateTemp(filepath.Dir(filePath), ".upload-*")
	if err != nil {
		return 0, fmt.Errorf("failed to create temporary upload file: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()

	limit := expectedSize
	if maxSize > 0 && (limit <= 0 || maxSize < limit) {
		limit = maxSize
	}
	copyReader := reader
	if limit > 0 && limit < math.MaxInt64 {
		copyReader = io.LimitReader(reader, limit+1)
	}
	written, err := io.Copy(temp, copyReader)
	if err != nil {
		return 0, fmt.Errorf("failed to write file: %w", err)
	}
	if maxSize > 0 && written > maxSize {
		return 0, fmt.Errorf("uploaded file exceeds maximum size of %d bytes", maxSize)
	}
	if expectedSize > 0 && written != expectedSize {
		return 0, fmt.Errorf("uploaded file size mismatch: expected %d, got %d", expectedSize, written)
	}
	if err := temp.Sync(); err != nil {
		return 0, fmt.Errorf("failed to sync uploaded file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return 0, fmt.Errorf("failed to close uploaded file: %w", err)
	}
	if err := os.Rename(tempPath, filePath); err != nil {
		return 0, fmt.Errorf("failed to publish uploaded file: %w", err)
	}
	keep = true
	return written, nil
}

func (s *UploadService) CompleteS3File(ctx context.Context, actor model.OverlayActor, fileUUID string) (*model.UploadFile, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, fmt.Errorf("upload file not found")
	}
	job, err := s.jobRepo.FindByUUID(file.JobUUID)
	if err != nil || !uploadJobAccessAllowed(job, actor) || job.Provider != model.UploadProviderS3 {
		return nil, fmt.Errorf("upload file not found")
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	size, err := storage.stat(ctx, file.StorageKey)
	if err != nil {
		return nil, err
	}
	if file.FileSize > 0 && size != file.FileSize {
		return nil, fmt.Errorf("uploaded object size mismatch: expected %d, got %d", file.FileSize, size)
	}
	if job.FileType == model.UploadFileTypeBed && size > maxBEDUploadBytes {
		return nil, fmt.Errorf("BED file exceeds maximum size of 20 MB")
	}
	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	allComplete, err := completeUploadMetadata(db.WithContext(ctx), file, job, size, file.StorageKey)
	if err != nil {
		return nil, err
	}
	if allComplete {
		NewSampleMatcher().run(context.Background())
	}
	return file, nil
}

// RetryS3File reissues a presigned URL for an interrupted file without
// creating a second data-asset record.
func (s *UploadService) RetryS3File(ctx context.Context, actor model.OverlayActor, fileUUID string) (*model.UploadFile, string, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, "", fmt.Errorf("upload file not found")
	}
	job, err := s.jobRepo.FindByUUID(file.JobUUID)
	if err != nil || !uploadJobAccessAllowed(job, actor) || job.Provider != model.UploadProviderS3 {
		return nil, "", fmt.Errorf("upload file not found")
	}
	if file.Status != model.FileStatusPending && file.Status != model.FileStatusFailed {
		return nil, "", fmt.Errorf("upload file is not retryable")
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, "", err
	}
	url, err := storage.presignUpload(ctx, file.StorageKey)
	if err != nil {
		return nil, "", err
	}
	file.Status = model.FileStatusPending
	if err := s.fileRepo.Update(file); err != nil {
		return nil, "", err
	}
	return file, url, nil
}

func completeUploadMetadata(db *gorm.DB, file *model.UploadFile, job *model.UploadJob, size int64, storageKey string) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("database is not initialized")
	}
	allComplete := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var lockedJob model.UploadJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedJob, job.ID).Error; err != nil {
			return fmt.Errorf("upload job not found: %w", err)
		}
		var lockedFile model.UploadFile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedFile, file.ID).Error; err != nil || lockedFile.JobID != lockedJob.ID {
			return fmt.Errorf("upload file not found")
		}
		if lockedFile.Status == model.FileStatusDeleted {
			return fmt.Errorf("upload file was deleted")
		}
		lockedFile.StorageKey = storageKey
		lockedFile.FileSize = size
		lockedFile.Status = model.FileStatusCompleted
		if err := tx.Save(&lockedFile).Error; err != nil {
			return fmt.Errorf("failed to update file record: %w", err)
		}
		var asset model.DataAsset
		if err := tx.Where("upload_file_id = ?", lockedFile.ID).First(&asset).Error; err != nil {
			return fmt.Errorf("data asset not found: %w", err)
		}
		asset.StorageKey = storageKey
		asset.FileSize = size
		asset.Status = model.FileStatusCompleted
		if err := tx.Save(&asset).Error; err != nil {
			return fmt.Errorf("failed to update data asset: %w", err)
		}
		var incomplete int64
		if err := tx.Model(&model.UploadFile{}).Where("job_id = ? AND status <> ?", lockedJob.ID, model.FileStatusCompleted).Count(&incomplete).Error; err != nil {
			return fmt.Errorf("failed to reconcile upload job: %w", err)
		}
		allComplete = incomplete == 0
		if allComplete {
			lockedJob.Status = model.UploadJobStatusCompleted
		} else {
			lockedJob.Status = model.UploadJobStatusUploading
		}
		if err := tx.Save(&lockedJob).Error; err != nil {
			return fmt.Errorf("failed to update upload job: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	file.StorageKey = storageKey
	file.FileSize = size
	file.Status = model.FileStatusCompleted
	if allComplete {
		job.Status = model.UploadJobStatusCompleted
	} else {
		job.Status = model.UploadJobStatusUploading
	}
	return allComplete, nil
}

func uploadJobAccessAllowed(job *model.UploadJob, actor model.OverlayActor) bool {
	if job == nil {
		return false
	}
	if actor.Role == string(model.SystemRoleSuperAdmin) {
		return true
	}
	if job.ExternalOrgID != "" {
		return actor.OrgID != "" && job.ExternalOrgID == actor.OrgID && job.UserID == actor.UserID
	}
	return job.UserID != 0 && job.UserID == actor.UserID
}

// uploadJobUseAllowed is broader than uploadJobAccessAllowed: completed data
// belongs to the organization and may be selected by another organization
// member, while upload lifecycle operations remain restricted to the uploader.
func uploadJobUseAllowed(job *model.UploadJob, actor model.OverlayActor) bool {
	if job == nil {
		return false
	}
	if actor.Role == string(model.SystemRoleSuperAdmin) {
		return true
	}
	if job.ExternalOrgID != "" {
		return actor.OrgID != "" && job.ExternalOrgID == actor.OrgID
	}
	return job.UserID != 0 && job.UserID == actor.UserID
}

func (s *UploadService) GetJob(ctx context.Context, actor model.OverlayActor, uuid string) (*model.UploadJob, []model.UploadFile, error) {
	job, err := s.jobRepo.FindByUUID(uuid)
	if err != nil {
		return nil, nil, fmt.Errorf("upload job not found: %w", err)
	}
	if !uploadJobAccessAllowed(job, actor) {
		return nil, nil, fmt.Errorf("upload job not found")
	}

	files, err := s.fileRepo.FindByJobID(job.ID)
	if err != nil {
		return job, nil, nil
	}

	return job, files, nil
}

func (s *UploadService) ListJobs(ctx context.Context, actor model.OverlayActor, query *model.UploadJobListQuery) ([]model.UploadJob, int64, error) {
	jobs, total, err := s.jobRepo.PaginateByQuery(query, actor)
	if err != nil {
		return nil, 0, err
	}

	return jobs, total, nil
}

func (s *UploadService) DeleteJob(ctx context.Context, actor model.OverlayActor, uuid string) error {
	job, err := s.jobRepo.FindByUUID(uuid)
	if err != nil {
		return fmt.Errorf("upload job not found: %w", err)
	}
	if !uploadJobAccessAllowed(job, actor) {
		return fmt.Errorf("upload job not found")
	}
	if job.Status == model.UploadJobStatusDeleted {
		return nil
	}
	db := database.GetDB()
	if db == nil {
		return fmt.Errorf("database is not initialized")
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked model.UploadJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, job.ID).Error; err != nil || !uploadJobAccessAllowed(&locked, actor) {
			return fmt.Errorf("upload job not found")
		}
		if locked.Status != model.UploadJobStatusDeleted {
			return tx.Model(&locked).Update("status", model.UploadJobStatusDeleting).Error
		}
		return nil
	}); err != nil {
		return err
	}

	files, err := s.fileRepo.FindByJobID(job.ID)
	if err != nil {
		return err
	}

	assetSvc := NewDataAssetService(s.cfg)
	for i := range files {
		asset, assetErr := s.assetRepo.FindByUploadFileID(files[i].ID)
		if assetErr == nil {
			if err := assetSvc.deleteAsset(ctx, asset); err != nil {
				return fmt.Errorf("failed to delete upload file %s: %w", files[i].UUID, err)
			}
			continue
		}
		if !errors.Is(assetErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to load data asset for upload file %s: %w", files[i].UUID, assetErr)
		}
		legacyAsset := &model.DataAsset{Provider: job.Provider, StorageKey: files[i].StorageKey}
		if err := assetSvc.deleteStoredObject(ctx, legacyAsset); err != nil {
			return fmt.Errorf("failed to delete legacy upload file %s: %w", files[i].UUID, err)
		}
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.UploadFile{}).Where("job_id = ?", job.ID).Update("status", model.FileStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Model(&model.UploadJob{}).Where("id = ?", job.ID).Update("status", model.UploadJobStatusDeleted).Error
	})
}

func (s *UploadService) GetJobFiles(ctx context.Context, jobID uint) ([]model.UploadFile, error) {
	return s.fileRepo.FindByJobID(jobID)
}

// ListFiles returns the file-level audit list (org/user scoped by the handler).
func (s *UploadService) ListFiles(ctx context.Context, query *model.UploadFileListQuery) (*model.UploadFileListResponse, error) {
	rows, total, err := s.fileRepo.PaginateFilesByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.UploadFileAuditResponse, len(rows))
	for i, row := range rows {
		items[i] = model.UploadFileAuditResponse{
			ID:                         row.UUID,
			DataAssetID:                row.DataAssetID,
			JobID:                      row.JobUUID,
			FileName:                   row.FileName,
			StoragePath:                row.StorageKey,
			FileSize:                   row.FileSize,
			ReadType:                   row.ReadType,
			Status:                     row.Status,
			OrgID:                      row.OrgID,
			UploadPolicyVersion:        row.UploadPolicyVersion,
			UploadPolicyAcknowledgedAt: row.UploadPolicyAcknowledgedAt,
			CreatedAt:                  row.CreatedAt.Format(time.RFC3339),
			UpdatedAt:                  row.UpdatedAt.Format(time.RFC3339),
		}
	}

	return &model.UploadFileListResponse{Total: total, Items: items}, nil
}

// GetFileStats returns the total count and total bytes of completed files
// under the same scope (for the /upload/files/stats aggregate endpoint).
func (s *UploadService) GetFileStats(ctx context.Context, query *model.UploadFileListQuery) (int64, int64, error) {
	return s.fileRepo.CompletedStorageStats(query)
}

func (s *UploadService) GetLocalFilePath(ctx context.Context, actor model.OverlayActor, fileUUID string) (string, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return "", fmt.Errorf("upload file not found: %w", err)
	}
	job, err := s.jobRepo.FindByUUID(file.JobUUID)
	if err != nil || !uploadJobAccessAllowed(job, actor) {
		return "", fmt.Errorf("upload file not found")
	}
	if file.Status != model.FileStatusCompleted {
		return "", fmt.Errorf("upload file is not completed")
	}

	return safeLocalUploadPath(s.cfg.Storage.LocalDir, file.StorageKey)
}

func safeLocalUploadPath(localDir, storageKey string) (string, error) {
	if strings.TrimSpace(storageKey) == "" {
		return "", fmt.Errorf("upload file path is empty")
	}
	base, err := filepath.Abs(localDir)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(storageKey)
	if err != nil {
		return "", err
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err == nil {
		base = resolvedBase
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, resolvedPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("upload file path escapes storage directory")
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("upload file is not a regular file")
	}
	return resolvedPath, nil
}
