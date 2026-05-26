package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
)

type UploadService struct {
	cfg      *config.Config
	jobRepo  *repository.UploadJobRepository
	fileRepo *repository.UploadFileRepository
	userRepo *repository.UserRepository
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
		cfg:      cfg,
		jobRepo:  repository.NewUploadJobRepository(),
		fileRepo: repository.NewUploadFileRepository(),
		userRepo: repository.NewUserRepository(),
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

func (s *UploadService) CreateJob(ctx context.Context, userID uint, req *model.UploadJobCreateRequest) (*model.UploadJob, []*model.UploadFile, []string, error) {
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

	jobUUID := uuid.New().String()
	job := &model.UploadJob{
		UUID:     jobUUID,
		UserID:   userID,
		SampleID: req.SampleID,
		Name:     req.Name,
		FileType: req.FileType,
		Provider: model.UploadProviderLocal,
		Status:   model.UploadJobStatusPending,
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
		storageKey := s.buildStorageKey(storageFolder, jobUUID, f.FileName)

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

		files = append(files, uploadFile)

		presignedURLs = append(presignedURLs, "")
	}

	return job, files, presignedURLs, nil
}

func (s *UploadService) buildStorageKey(storageFolder, jobUUID, fileName string) string {
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

	s.syncJobStatus(job)

	return existingFile, nil
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

	return file.StorageKey, nil
}
