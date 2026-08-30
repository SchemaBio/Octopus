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
	if existingFile.Status != model.FileStatusPending && existingFile.Status != model.FileStatusFailed && existingFile.Status != model.FileStatusUploading {
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

// StartUploadFile marks a file as actively uploading. It is deliberately a
// separate call from job creation so a user can create a draft upload without
// making the data center show a false "uploading" state. Both S3/COS and the
// local streaming path use this lifecycle marker.
func (s *UploadService) StartUploadFile(ctx context.Context, actor model.OverlayActor, fileUUID string) (*model.UploadFile, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, fmt.Errorf("upload file not found")
	}
	job, err := s.jobRepo.FindByUUID(file.JobUUID)
	if err != nil || !uploadJobAccessAllowed(job, actor) {
		return nil, fmt.Errorf("upload file not found")
	}
	if job.Status == model.UploadJobStatusDeleting || job.Status == model.UploadJobStatusDeleted {
		return nil, fmt.Errorf("upload file was deleted")
	}
	if file.Status == model.FileStatusCompleted {
		return nil, fmt.Errorf("upload file is already completed")
	}
	if file.Status == model.FileStatusDeleted || file.Status == model.FileStatusDeleting {
		return nil, fmt.Errorf("upload file was deleted")
	}
	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedFile model.UploadFile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedFile, file.ID).Error; err != nil {
			return fmt.Errorf("upload file not found")
		}
		var lockedJob model.UploadJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedJob, job.ID).Error; err != nil || !uploadJobAccessAllowed(&lockedJob, actor) {
			return fmt.Errorf("upload file not found")
		}
		if lockedFile.Status == model.FileStatusCompleted {
			return fmt.Errorf("upload file is already completed")
		}
		if lockedFile.Status == model.FileStatusDeleted || lockedFile.Status == model.FileStatusDeleting ||
			lockedJob.Status == model.UploadJobStatusDeleting || lockedJob.Status == model.UploadJobStatusDeleted {
			return fmt.Errorf("upload file was deleted")
		}
		lockedFile.Status = model.FileStatusUploading
		if err := tx.Save(&lockedFile).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.DataAsset{}).Where("upload_file_id = ?", lockedFile.ID).Update("status", model.FileStatusUploading).Error; err != nil {
			return err
		}
		lockedJob.Status = model.UploadJobStatusUploading
		if err := tx.Save(&lockedJob).Error; err != nil {
			return err
		}
		*file = lockedFile
		*job = lockedJob
		return nil
	})
	if err != nil {
		return nil, err
	}
	return file, nil
}

// StartS3File is kept for service-level callers that need to assert an
// object-store upload specifically. The HTTP lifecycle endpoint intentionally
// uses StartUploadFile so self-deployed local storage gets the same status
// semantics without enabling multipart.
func (s *UploadService) StartS3File(ctx context.Context, actor model.OverlayActor, fileUUID string) (*model.UploadFile, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, fmt.Errorf("upload file not found")
	}
	job, err := s.jobRepo.FindByUUID(file.JobUUID)
	if err != nil || !uploadJobAccessAllowed(job, actor) || job.Provider != model.UploadProviderS3 {
		return nil, fmt.Errorf("upload file not found")
	}
	return s.StartUploadFile(ctx, actor, fileUUID)
}

func multipartTotalParts(fileSize, partSize int64) int {
	if fileSize <= 0 || partSize <= 0 {
		return 0
	}
	return int((fileSize + partSize - 1) / partSize)
}

// validateMultipartPartMetadata centralizes the part-number and exact-size
// checks used before recording an ETag. Keeping this independent of the
// database makes the multipart contract easy to exercise in unit tests and
// prevents a client from marking a short/non-final part as complete.
func validateMultipartPartMetadata(partNumber int, etag string, size, fileSize, partSize int64) error {
	total := multipartTotalParts(fileSize, partSize)
	if partNumber < 1 || partNumber > total || strings.TrimSpace(etag) == "" || size <= 0 || size > partSize {
		return fmt.Errorf("invalid multipart part")
	}
	if partNumber == total {
		remaining := fileSize - int64(total-1)*partSize
		if size != remaining {
			return fmt.Errorf("last multipart part size mismatch: expected %d, got %d", remaining, size)
		}
	} else if size != partSize {
		return fmt.Errorf("multipart part size mismatch: expected %d, got %d", partSize, size)
	}
	return nil
}

func validateRecordedMultipartPart(existing *model.UploadMultipartPart, etag string, size int64, partNumber int) error {
	if existing != nil && existing.ETag == strings.TrimSpace(etag) && existing.Size == size {
		return nil
	}
	return fmt.Errorf("multipart part %d was already recorded with different ETag or size", partNumber)
}

func collectMultipartParts(rows []model.UploadMultipartPart, total int, fileSize, partSize int64) ([]multipartPart, error) {
	if len(rows) != total {
		return nil, fmt.Errorf("multipart upload is incomplete: received %d of %d parts", len(rows), total)
	}
	parts := make([]multipartPart, 0, len(rows))
	for index, row := range rows {
		if row.PartNumber != index+1 {
			return nil, fmt.Errorf("multipart part %d is missing", index+1)
		}
		if err := validateMultipartPartMetadata(row.PartNumber, row.ETag, row.Size, fileSize, partSize); err != nil {
			return nil, err
		}
		parts = append(parts, multipartPart{Number: row.PartNumber, ETag: row.ETag, Size: row.Size})
	}
	return parts, nil
}

func (s *UploadService) multipartScope(ctx context.Context, actor model.OverlayActor, fileUUID string) (*model.UploadFile, *model.UploadJob, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, nil, fmt.Errorf("upload file not found")
	}
	job, err := s.jobRepo.FindByUUID(file.JobUUID)
	if err != nil || !uploadJobAccessAllowed(job, actor) || job.Provider != model.UploadProviderS3 {
		return nil, nil, fmt.Errorf("upload file not found")
	}
	if err := validateMultipartStorageKey(file, job); err != nil {
		return nil, nil, err
	}
	return file, job, nil
}

// validateMultipartWritable is intentionally separate from multipartScope:
// CompleteMultipart must remain able to return an idempotent response for a
// session that has already completed, while signing or recording another part
// must be rejected once the file/job is completed or being deleted.
func validateMultipartWritable(file *model.UploadFile, job *model.UploadJob) error {
	if file == nil || job == nil {
		return fmt.Errorf("upload file not found")
	}
	if file.Status == model.FileStatusCompleted {
		return fmt.Errorf("upload file is already completed")
	}
	if file.Status == model.FileStatusDeleting || file.Status == model.FileStatusDeleted ||
		job.Status == model.UploadJobStatusDeleting || job.Status == model.UploadJobStatusDeleted {
		return fmt.Errorf("upload file was deleted")
	}
	return nil
}

// validateMultipartStorageKey keeps every multipart operation inside the
// server-generated tenant layout.  The browser only receives file/session
// UUIDs, but this check also protects against stale or manually-corrupted
// metadata before a presigned URL is created.
func validateMultipartStorageKey(file *model.UploadFile, job *model.UploadJob) error {
	if file == nil || job == nil {
		return fmt.Errorf("upload file not found")
	}
	key := strings.TrimSpace(file.StorageKey)
	if key == "" || strings.Contains(key, `\`) || path.IsAbs(key) || path.Clean(key) != key {
		return fmt.Errorf("upload file has an invalid storage key")
	}
	if job.ExternalOrgID != "" {
		prefix, err := tenantStoragePrefix(job.ExternalOrgID)
		if err != nil {
			return fmt.Errorf("upload file has an invalid tenant")
		}
		if !strings.HasPrefix(key, prefix+"/") {
			return fmt.Errorf("upload file is outside the tenant storage prefix")
		}
	} else if !strings.HasPrefix(key, "uploads/") {
		return fmt.Errorf("upload file is outside the upload storage prefix")
	}
	return nil
}

// InitMultipart creates or resumes a COS/S3 multipart session. The object
// store upload is created before the database row; on a persistence failure we
// abort the remote session to avoid leaked parts.
func (s *UploadService) InitMultipart(ctx context.Context, actor model.OverlayActor, fileUUID string) (*model.MultipartInitResponse, error) {
	file, job, err := s.multipartScope(ctx, actor, fileUUID)
	if err != nil {
		return nil, err
	}
	if err := validateMultipartWritable(file, job); err != nil {
		return nil, err
	}
	if file.FileSize < model.MultipartThresholdBytes {
		return nil, fmt.Errorf("multipart upload is only required for files at least 64 MiB")
	}
	partSize := model.MultipartPartSizeBytes
	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	var session model.UploadMultipartSession
	if err := db.WithContext(ctx).Where("file_uuid = ? AND status = ?", file.UUID, "active").First(&session).Error; err == nil {
		if session.FileSize != file.FileSize || session.PartSize != partSize || session.StorageKey != file.StorageKey {
			return nil, fmt.Errorf("multipart session metadata does not match upload file")
		}
		// A previous request can have created the remote session and then
		// returned before its status transaction completed.  Re-entering through
		// InitMultipart must restore the durable uploading state instead of
		// leaving the data center stuck at pending.
		if _, err := s.StartUploadFile(ctx, actor, file.UUID); err != nil {
			return nil, err
		}
		return s.multipartInitResponse(ctx, &session), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	uploadID, err := storage.createMultipart(ctx, file.StorageKey, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	session = model.UploadMultipartSession{UUID: uuid.New().String(), FileUUID: file.UUID, JobUUID: job.UUID, StorageKey: file.StorageKey, UploadID: uploadID, PartSize: partSize, FileSize: file.FileSize, Status: "active"}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedJob model.UploadJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedJob, job.ID).Error; err != nil {
			return err
		}
		if lockedJob.Status == model.UploadJobStatusDeleting || lockedJob.Status == model.UploadJobStatusDeleted {
			return fmt.Errorf("upload file was deleted")
		}
		var lockedFile model.UploadFile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedFile, file.ID).Error; err != nil {
			return err
		}
		if lockedFile.Status == model.FileStatusCompleted || lockedFile.Status == model.FileStatusDeleting || lockedFile.Status == model.FileStatusDeleted {
			return fmt.Errorf("upload file was deleted")
		}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		if err := tx.Model(&lockedFile).Update("status", model.FileStatusUploading).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.DataAsset{}).Where("upload_file_id = ?", file.ID).Update("status", model.FileStatusUploading).Error; err != nil {
			return err
		}
		return tx.Model(&lockedJob).Update("status", model.UploadJobStatusUploading).Error
	}); err != nil {
		_ = storage.abortMultipart(ctx, file.StorageKey, uploadID)
		// Another request may have won the active-session race. Return that
		// durable session instead of surfacing a duplicate-key error or asking
		// the browser to start a second upload.
		var existing model.UploadMultipartSession
		if lookupErr := db.WithContext(ctx).Where("file_uuid = ? AND status = ?", file.UUID, "active").First(&existing).Error; lookupErr == nil {
			if _, startErr := s.StartUploadFile(ctx, actor, file.UUID); startErr != nil {
				return nil, startErr
			}
			return s.multipartInitResponse(ctx, &existing), nil
		}
		return nil, fmt.Errorf("persist multipart session: %w", err)
	}
	return s.multipartInitResponse(ctx, &session), nil
}

func (s *UploadService) multipartInitResponse(ctx context.Context, session *model.UploadMultipartSession) *model.MultipartInitResponse {
	response := &model.MultipartInitResponse{SessionID: session.UUID, FileID: session.FileUUID, PartSize: session.PartSize, TotalParts: multipartTotalParts(session.FileSize, session.PartSize), CompletedParts: []int{}}
	if db := database.GetDB(); db != nil {
		var parts []model.UploadMultipartPart
		if db.WithContext(ctx).Where("session_uuid = ?", session.UUID).Order("part_number ASC").Find(&parts).Error == nil {
			for _, part := range parts {
				response.CompletedParts = append(response.CompletedParts, part.PartNumber)
			}
		}
	}
	return response
}

func (s *UploadService) PresignMultipartParts(ctx context.Context, actor model.OverlayActor, fileUUID, sessionUUID string, partNumbers []int) (*model.MultipartPresignResponse, error) {
	file, job, err := s.multipartScope(ctx, actor, fileUUID)
	if err != nil {
		return nil, err
	}
	if err := validateMultipartWritable(file, job); err != nil {
		return nil, err
	}
	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	var session model.UploadMultipartSession
	if err := db.WithContext(ctx).Where("uuid = ? AND file_uuid = ? AND job_uuid = ? AND status = ?", sessionUUID, file.UUID, job.UUID, "active").First(&session).Error; err != nil {
		return nil, fmt.Errorf("multipart session not found")
	}
	if session.FileSize != file.FileSize || session.StorageKey != file.StorageKey {
		return nil, fmt.Errorf("multipart session metadata does not match upload file")
	}
	total := multipartTotalParts(session.FileSize, session.PartSize)
	seen := make(map[int]struct{}, len(partNumbers))
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	result := &model.MultipartPresignResponse{SessionID: session.UUID, Parts: make([]model.MultipartPresignItem, 0, len(partNumbers))}
	for _, number := range partNumbers {
		if number < 1 || number > total {
			return nil, fmt.Errorf("multipart part number %d is out of range", number)
		}
		if _, ok := seen[number]; ok {
			continue
		}
		seen[number] = struct{}{}
		url, err := storage.presignUploadPart(ctx, file.StorageKey, session.UploadID, number)
		if err != nil {
			return nil, err
		}
		result.Parts = append(result.Parts, model.MultipartPresignItem{PartNumber: number, URL: url})
	}
	return result, nil
}

func (s *UploadService) RecordMultipartPart(ctx context.Context, actor model.OverlayActor, fileUUID, sessionUUID string, req *model.MultipartPartRequest) error {
	if req == nil {
		return fmt.Errorf("invalid multipart part")
	}
	file, job, err := s.multipartScope(ctx, actor, fileUUID)
	if err != nil {
		return err
	}
	if err := validateMultipartWritable(file, job); err != nil {
		return err
	}
	db := database.GetDB()
	if db == nil {
		return fmt.Errorf("database is not initialized")
	}
	var session model.UploadMultipartSession
	if err := db.WithContext(ctx).Where("uuid = ? AND file_uuid = ? AND job_uuid = ? AND status = ?", sessionUUID, file.UUID, job.UUID, "active").First(&session).Error; err != nil {
		return fmt.Errorf("multipart session not found")
	}
	if session.FileSize != file.FileSize || session.StorageKey != file.StorageKey {
		return fmt.Errorf("multipart session metadata does not match upload file")
	}
	if err := validateMultipartPartMetadata(req.PartNumber, req.ETag, req.Size, session.FileSize, session.PartSize); err != nil {
		return err
	}
	etag := strings.TrimSpace(req.ETag)
	var existing model.UploadMultipartPart
	lookupErr := db.WithContext(ctx).Where("session_uuid = ? AND part_number = ?", session.UUID, req.PartNumber).First(&existing).Error
	if lookupErr == nil {
		return validateRecordedMultipartPart(&existing, etag, req.Size, req.PartNumber)
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return lookupErr
	}
	part := &model.UploadMultipartPart{SessionUUID: session.UUID, PartNumber: req.PartNumber, ETag: etag, Size: req.Size}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(part).Error; err != nil {
		return err
	}
	// Another request may have won the insert between the lookup above and our
	// create. Re-read the durable value and accept only an identical duplicate;
	// a different ETag/size must never silently replace an uploaded part.
	if err := db.WithContext(ctx).Where("session_uuid = ? AND part_number = ?", session.UUID, req.PartNumber).First(&existing).Error; err != nil {
		return err
	}
	return validateRecordedMultipartPart(&existing, etag, req.Size, req.PartNumber)
}

func (s *UploadService) CompleteMultipart(ctx context.Context, actor model.OverlayActor, fileUUID, sessionUUID string) (*model.UploadFile, error) {
	file, job, err := s.multipartScope(ctx, actor, fileUUID)
	if err != nil {
		return nil, err
	}
	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	var session model.UploadMultipartSession
	if err := db.WithContext(ctx).Where("uuid = ? AND file_uuid = ? AND job_uuid = ?", sessionUUID, file.UUID, job.UUID).First(&session).Error; err != nil {
		return nil, fmt.Errorf("multipart session not found")
	}
	if session.FileSize != file.FileSize || session.StorageKey != file.StorageKey {
		return nil, fmt.Errorf("multipart session metadata does not match upload file")
	}
	if session.Status != "active" {
		if session.Status == "completed" && file.Status == model.FileStatusCompleted {
			// A client may retry the final request after a response timeout. The
			// object and database are already committed, so completion is
			// idempotent and should simply return the durable file state.
			return file, nil
		}
		return nil, fmt.Errorf("multipart session is not active")
	}
	if err := validateMultipartWritable(file, job); err != nil {
		return nil, err
	}
	total := multipartTotalParts(session.FileSize, session.PartSize)
	var rows []model.UploadMultipartPart
	if err := db.WithContext(ctx).Where("session_uuid = ?", session.UUID).Order("part_number ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	parts, err := collectMultipartParts(rows, total, session.FileSize, session.PartSize)
	if err != nil {
		return nil, err
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	// If a previous request completed the remote multipart upload but failed
	// before committing the database transaction, HEAD lets us recover without
	// issuing CompleteMultipartUpload a second time (which would return
	// NoSuchUpload on COS/S3). Otherwise complete it now, and verify the final
	// object size before changing any metadata.
	size, statErr := storage.stat(ctx, session.StorageKey)
	if statErr != nil || size != session.FileSize {
		if completeErr := storage.completeMultipart(ctx, session.StorageKey, session.UploadID, parts); completeErr != nil {
			size, statErr = storage.stat(ctx, session.StorageKey)
			if statErr != nil || size != session.FileSize {
				return nil, completeErr
			}
		} else {
			size, statErr = storage.stat(ctx, session.StorageKey)
			if statErr != nil {
				return nil, statErr
			}
		}
	}
	if size != session.FileSize {
		return nil, fmt.Errorf("multipart object size mismatch: expected %d, got %d", session.FileSize, size)
	}
	now := time.Now().UTC()
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedJob model.UploadJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedJob, job.ID).Error; err != nil {
			return err
		}
		var lockedFile model.UploadFile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedFile, file.ID).Error; err != nil {
			return err
		}
		if lockedFile.Status == model.FileStatusDeleted || lockedFile.Status == model.FileStatusDeleting ||
			lockedJob.Status == model.UploadJobStatusDeleting || lockedJob.Status == model.UploadJobStatusDeleted {
			return fmt.Errorf("upload file was deleted")
		}
		if lockedFile.Status == model.FileStatusCompleted {
			// A concurrent completion may have committed the metadata after this
			// request finished the remote multipart upload. Treat the second call as
			// an idempotent success instead of asking the browser to restart.
			if lockedFile.FileSize != size {
				return fmt.Errorf("uploaded object size mismatch: expected %d, got %d", lockedFile.FileSize, size)
			}
			if err := tx.Model(&session).Updates(map[string]interface{}{"status": "completed", "completed_at": now}).Error; err != nil {
				return err
			}
			*file = lockedFile
			*job = lockedJob
			return nil
		}
		if err := tx.Model(&lockedFile).Updates(map[string]interface{}{"status": model.FileStatusCompleted, "file_size": size, "storage_key": session.StorageKey}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.DataAsset{}).Where("upload_file_id = ?", lockedFile.ID).Updates(map[string]interface{}{"status": model.FileStatusCompleted, "file_size": size, "storage_key": session.StorageKey}).Error; err != nil {
			return err
		}
		status, err := reconcileUploadJobStatus(tx, lockedJob.ID)
		if err != nil {
			return err
		}
		lockedJob.Status = status
		if err := tx.Model(&session).Updates(map[string]interface{}{"status": "completed", "completed_at": now}).Error; err != nil {
			return err
		}
		*file = lockedFile
		*job = lockedJob
		return nil
	}); err != nil {
		return nil, err
	}
	return file, nil
}

func (s *UploadService) AbortMultipart(ctx context.Context, actor model.OverlayActor, fileUUID, sessionUUID string) error {
	file, job, err := s.multipartScope(ctx, actor, fileUUID)
	if err != nil {
		return err
	}
	db := database.GetDB()
	if db == nil {
		return fmt.Errorf("database is not initialized")
	}
	var session model.UploadMultipartSession
	if err := db.WithContext(ctx).Where("uuid = ? AND file_uuid = ? AND job_uuid = ?", sessionUUID, file.UUID, job.UUID).First(&session).Error; err != nil {
		return fmt.Errorf("multipart session not found")
	}
	if session.FileSize != file.FileSize || session.StorageKey != file.StorageKey {
		return fmt.Errorf("multipart session metadata does not match upload file")
	}
	if session.Status == "aborted" {
		return nil
	}
	if session.Status != "active" {
		return fmt.Errorf("multipart session is not active")
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return err
	}
	if err := storage.abortMultipart(ctx, session.StorageKey, session.UploadID); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Completion and cancellation can finish their object-store operation in
		// either order. Lock the durable rows before changing metadata so a late
		// abort cannot downgrade a file that has already been committed.
		var lockedJob model.UploadJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedJob, job.ID).Error; err != nil {
			return err
		}
		var lockedFile model.UploadFile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedFile, file.ID).Error; err != nil {
			return err
		}
		if lockedJob.Status == model.UploadJobStatusDeleting || lockedJob.Status == model.UploadJobStatusDeleted ||
			lockedFile.Status == model.FileStatusDeleting || lockedFile.Status == model.FileStatusDeleted {
			return fmt.Errorf("upload file was deleted")
		}
		if lockedFile.Status == model.FileStatusCompleted {
			return fmt.Errorf("upload file is already completed")
		}
		var lockedSession model.UploadMultipartSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedSession, session.ID).Error; err != nil {
			return err
		}
		if lockedSession.Status == "aborted" {
			return nil
		}
		if lockedSession.Status != "active" {
			return fmt.Errorf("multipart session is not active")
		}
		if err := tx.Model(&lockedSession).Update("status", "aborted").Error; err != nil {
			return err
		}
		if err := tx.Model(&lockedFile).Update("status", model.FileStatusFailed).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.DataAsset{}).Where("upload_file_id = ?", file.ID).Update("status", model.FileStatusFailed).Error; err != nil {
			return err
		}
		_, err = reconcileUploadJobStatus(tx, lockedJob.ID)
		return err
	})
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
	if job.Status == model.UploadJobStatusDeleting || job.Status == model.UploadJobStatusDeleted {
		return nil, "", fmt.Errorf("upload file was deleted")
	}
	if file.Status != model.FileStatusPending && file.Status != model.FileStatusFailed && file.Status != model.FileStatusUploading {
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
	reconciledStatus := model.UploadJobStatusFailed
	err := db.Transaction(func(tx *gorm.DB) error {
		var lockedJob model.UploadJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedJob, job.ID).Error; err != nil {
			return fmt.Errorf("upload job not found: %w", err)
		}
		var lockedFile model.UploadFile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedFile, file.ID).Error; err != nil || lockedFile.JobID != lockedJob.ID {
			return fmt.Errorf("upload file not found")
		}
		if lockedJob.Status == model.UploadJobStatusDeleting || lockedJob.Status == model.UploadJobStatusDeleted ||
			lockedFile.Status == model.FileStatusDeleting || lockedFile.Status == model.FileStatusDeleted {
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
		status, err := reconcileUploadJobStatus(tx, lockedJob.ID)
		if err != nil {
			return fmt.Errorf("failed to reconcile upload job: %w", err)
		}
		allComplete = status == model.UploadJobStatusCompleted
		reconciledStatus = status
		lockedJob.Status = status
		return nil
	})
	if err != nil {
		return false, err
	}
	file.StorageKey = storageKey
	file.FileSize = size
	file.Status = model.FileStatusCompleted
	job.Status = reconciledStatus
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
	if job.Provider == model.UploadProviderS3 {
		storage, storageErr := newS3Storage(ctx, s.cfg.Storage)
		if storageErr != nil {
			return fmt.Errorf("failed to initialize object storage for multipart cleanup: %w", storageErr)
		}
		var sessions []model.UploadMultipartSession
		if queryErr := db.WithContext(ctx).Where("job_uuid = ? AND status = ?", job.UUID, "active").Find(&sessions).Error; queryErr != nil {
			return fmt.Errorf("failed to load active multipart sessions: %w", queryErr)
		}
		for i := range sessions {
			if abortErr := storage.abortMultipart(ctx, sessions[i].StorageKey, sessions[i].UploadID); abortErr != nil {
				return fmt.Errorf("failed to abort multipart session %s: %w", sessions[i].UUID, abortErr)
			}
			if updateErr := db.WithContext(ctx).Model(&sessions[i]).Update("status", "aborted").Error; updateErr != nil {
				return fmt.Errorf("failed to persist aborted multipart session %s: %w", sessions[i].UUID, updateErr)
			}
		}
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
