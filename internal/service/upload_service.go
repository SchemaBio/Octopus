package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
)

type UploadService struct {
	cfg       *config.Config
	jobRepo   *repository.UploadJobRepository
	fileRepo  *repository.UploadFileRepository
	assetRepo *repository.DataAssetRepository
	userRepo  *repository.UserRepository
}

var safeUploadFilename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+=-]{0,254}$`)

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

	for _, f := range req.Files {
		if err := validateUploadFilename(f.FileName); err != nil {
			return nil, nil, nil, err
		}
		if f.FileSize <= 0 {
			return nil, nil, nil, fmt.Errorf("file size must be positive")
		}
		if s.cfg.Storage.MaxSizeMB > 0 && f.FileSize > int64(s.cfg.Storage.MaxSizeMB)*1024*1024 {
			return nil, nil, nil, fmt.Errorf("file %s exceeds maximum size of %d MB", f.FileName, s.cfg.Storage.MaxSizeMB)
		}
	}

	provider := model.UploadProvider(s.cfg.Storage.Provider)
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
		UUID:          jobUUID,
		UserID:        userID,
		ExternalOrgID: orgID,
		SampleID:      req.SampleID,
		Name:          req.Name,
		FileType:      req.FileType,
		Provider:      provider,
		Status:        model.UploadJobStatusPending,
	}

	if err := s.jobRepo.Create(job); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create upload job: %w", err)
	}

	storageFolder, err := s.getUserStorageFolder(ctx, userID)
	if err != nil {
		return nil, nil, nil, err
	}

	files := make([]*model.UploadFile, 0, len(req.Files))
	presignedURLs := make([]string, 0, len(req.Files))

	for _, f := range req.Files {
		fileUUID := uuid.New().String()
		storageKey := s.buildStorageKey(provider, storageFolder, jobUUID, f.FileName)

		uploadFile := &model.UploadFile{
			UUID:       fileUUID,
			JobID:      job.ID,
			JobUUID:    jobUUID,
			FileName:   f.FileName,
			StorageKey: storageKey,
			FileSize:   f.FileSize,
			ReadType:   f.ReadType,
			Status:     model.FileStatusPending,
		}

		if err := s.fileRepo.Create(uploadFile); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create upload file record: %w", err)
		}
		var expiresAt *time.Time
		if s.cfg.Storage.RetentionDays > 0 {
			value := time.Now().AddDate(0, 0, s.cfg.Storage.RetentionDays)
			expiresAt = &value
		}
		asset := &model.DataAsset{
			UUID: fileUUID, ExternalOrgID: orgID, CreatedBy: userID,
			UploadFileID: &uploadFile.ID, Provider: provider, StorageKey: storageKey,
			FileName: f.FileName, FileSize: f.FileSize, ReadType: f.ReadType,
			Status: model.FileStatusPending, Source: model.DataAssetSourceUpload, ExpiresAt: expiresAt,
		}
		if err := s.assetRepo.Create(asset); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to register data asset: %w", err)
		}

		files = append(files, uploadFile)

		if objectStore != nil {
			url, err := objectStore.presignUpload(ctx, storageKey)
			if err != nil {
				return nil, nil, nil, err
			}
			presignedURLs = append(presignedURLs, url)
		} else {
			presignedURLs = append(presignedURLs, "")
		}
	}

	return job, files, presignedURLs, nil
}

func (s *UploadService) buildStorageKey(provider model.UploadProvider, storageFolder, jobUUID, fileName string) string {
	if provider == model.UploadProviderS3 {
		return path.Join("uploads", storageFolder, jobUUID, fileName)
	}
	return filepath.Join(storageFolder, jobUUID, fileName)
}

func (s *UploadService) SaveLocalFile(ctx context.Context, userID uint, fileUUID string, reader io.Reader, fileSize int64) (*model.UploadFile, error) {
	existingFile, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, fmt.Errorf("upload file record not found: %w", err)
	}

	storageFolder, err := s.getUserStorageFolder(ctx, userID)
	if err != nil {
		return nil, err
	}

	job, err := s.jobRepo.FindByUUID(existingFile.JobUUID)
	if err != nil {
		return nil, fmt.Errorf("upload job not found: %w", err)
	}
	if job.UserID != userID {
		return nil, fmt.Errorf("upload file does not belong to current user")
	}
	if job.Provider != model.UploadProviderLocal {
		return nil, fmt.Errorf("upload job does not use local storage")
	}
	if err := validateUploadFilename(existingFile.FileName); err != nil {
		return nil, err
	}

	dir := filepath.Join(s.cfg.Storage.LocalDir, storageFolder, job.UUID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	filePath := filepath.Join(dir, existingFile.FileName)
	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, reader)
	if err != nil {
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to write file: %w", err)
	}
	if s.cfg.Storage.MaxSizeMB > 0 && written > int64(s.cfg.Storage.MaxSizeMB)*1024*1024 {
		os.Remove(filePath)
		return nil, fmt.Errorf("file exceeds maximum size of %d MB", s.cfg.Storage.MaxSizeMB)
	}

	existingFile.StorageKey = filePath
	existingFile.FileSize = written
	existingFile.Status = model.FileStatusCompleted
	if err := s.fileRepo.Update(existingFile); err != nil {
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to update file record: %w", err)
	}
	if asset, err := s.assetRepo.FindByUploadFileID(existingFile.ID); err == nil {
		asset.StorageKey = filePath
		asset.FileSize = written
		asset.Status = model.FileStatusCompleted
		if err := s.assetRepo.Update(asset); err != nil {
			return nil, fmt.Errorf("failed to update data asset: %w", err)
		}
	}

	s.syncJobStatus(job)

	return existingFile, nil
}

func (s *UploadService) CompleteS3File(ctx context.Context, userID uint, fileUUID string) (*model.UploadFile, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, fmt.Errorf("upload file not found")
	}
	job, err := s.jobRepo.FindByUUID(file.JobUUID)
	if err != nil || job.UserID != userID || job.Provider != model.UploadProviderS3 {
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
	file.FileSize = size
	file.Status = model.FileStatusCompleted
	if err := s.fileRepo.Update(file); err != nil {
		return nil, err
	}
	asset, err := s.assetRepo.FindByUploadFileID(file.ID)
	if err != nil {
		return nil, fmt.Errorf("data asset not found")
	}
	asset.FileSize = size
	asset.Status = model.FileStatusCompleted
	if err := s.assetRepo.Update(asset); err != nil {
		return nil, err
	}
	s.syncJobStatus(job)
	return file, nil
}

func (s *UploadService) syncJobStatus(job *model.UploadJob) {
	allFiles, _ := s.fileRepo.FindByJobID(job.ID)
	allComplete := true
	for _, f := range allFiles {
		if f.Status != model.FileStatusCompleted {
			allComplete = false
			break
		}
	}
	if allComplete {
		job.Status = model.UploadJobStatusCompleted
	} else {
		job.Status = model.UploadJobStatusUploading
	}
	s.jobRepo.Update(job)
}

func (s *UploadService) GetJob(ctx context.Context, userID uint, uuid string) (*model.UploadJob, []model.UploadFile, error) {
	job, err := s.jobRepo.FindByUUID(uuid)
	if err != nil {
		return nil, nil, fmt.Errorf("upload job not found: %w", err)
	}
	if job.UserID != userID {
		return nil, nil, fmt.Errorf("upload job not found")
	}

	files, err := s.fileRepo.FindByJobID(job.ID)
	if err != nil {
		return job, nil, nil
	}

	return job, files, nil
}

func (s *UploadService) ListJobs(ctx context.Context, userID uint, query *model.UploadJobListQuery) ([]model.UploadJob, int64, error) {
	jobs, total, err := s.jobRepo.PaginateByQuery(query, userID)
	if err != nil {
		return nil, 0, err
	}

	return jobs, total, nil
}

func (s *UploadService) DeleteJob(ctx context.Context, userID uint, uuid string) error {
	job, err := s.jobRepo.FindByUUID(uuid)
	if err != nil {
		return fmt.Errorf("upload job not found: %w", err)
	}
	if job.UserID != userID {
		return fmt.Errorf("upload job not found")
	}

	files, err := s.fileRepo.FindByJobID(job.ID)
	if err != nil {
		return err
	}

	for _, file := range files {
		os.Remove(file.StorageKey)
	}

	if err := s.fileRepo.DeleteByJobID(job.ID); err != nil {
		return err
	}

	return s.jobRepo.Delete(job.ID)
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
			ID:          row.UUID,
			JobID:       row.JobUUID,
			FileName:    row.FileName,
			StoragePath: row.StorageKey,
			FileSize:    row.FileSize,
			ReadType:    row.ReadType,
			Status:      row.Status,
			OrgID:       row.OrgID,
			CreatedAt:   row.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   row.UpdatedAt.Format(time.RFC3339),
		}
	}

	return &model.UploadFileListResponse{Total: total, Items: items}, nil
}

// GetFileStats returns the total count and total bytes of completed files
// under the same scope (for the /upload/files/stats aggregate endpoint).
func (s *UploadService) GetFileStats(ctx context.Context, query *model.UploadFileListQuery) (int64, int64, error) {
	_, total, err := s.fileRepo.PaginateFilesByQuery(query)
	if err != nil {
		return 0, 0, err
	}
	bytes, err := s.fileRepo.SumFileSize(query)
	if err != nil {
		return total, 0, err
	}
	return total, bytes, nil
}

func (s *UploadService) GetLocalFilePath(ctx context.Context, userID uint, fileUUID string) (string, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return "", fmt.Errorf("upload file not found: %w", err)
	}
	job, err := s.jobRepo.FindByUUID(file.JobUUID)
	if err != nil || job.UserID != userID {
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
