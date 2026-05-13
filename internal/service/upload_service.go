package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
)

type UploadService struct {
	cfg       *config.Config
	jobRepo   *repository.UploadJobRepository
	fileRepo  *repository.UploadFileRepository
	userRepo  *repository.UserRepository
	cosClient *COSClient
}

func NewUploadService(cfg *config.Config) *UploadService {
	svc := &UploadService{
		cfg:      cfg,
		jobRepo:  repository.NewUploadJobRepository(),
		fileRepo: repository.NewUploadFileRepository(),
		userRepo: repository.NewUserRepository(),
	}

	if cfg.Storage.Provider == "cos" && cfg.Storage.COSSecretID != "" {
		client, err := NewCOSClient(&cfg.Storage)
		if err == nil {
			svc.cosClient = client
		}
	}

	return svc
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
	jobUUID := uuid.New().String()
	job := &model.UploadJob{
		UUID:     jobUUID,
		UserID:   userID,
		SampleID: req.SampleID,
		Name:     req.Name,
		FileType: req.FileType,
		Provider: req.Provider,
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

		if req.Provider == model.UploadProviderCOS && s.cosClient != nil {
			presignedURL, err := s.cosClient.GeneratePresignedPutURL(ctx, storageKey)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to generate presigned URL for %s: %w", f.FileName, err)
			}
			presignedURLs = append(presignedURLs, presignedURL)
		} else {
			presignedURLs = append(presignedURLs, "")
		}
	}

	return job, files, presignedURLs, nil
}

func (s *UploadService) buildStorageKey(storageFolder, jobUUID, fileName string) string {
	return s.cfg.Storage.COSPrefix + storageFolder + "/" + jobUUID + "/" + fileName
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

func (s *UploadService) CompleteCOSFile(ctx context.Context, jobUUID, fileUUID string, fileSize int64) (*model.UploadFile, error) {
	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return nil, fmt.Errorf("upload file not found: %w", err)
	}

	if file.JobUUID != jobUUID {
		return nil, fmt.Errorf("file does not belong to the specified job")
	}

	exists, err := s.cosClient.ObjectExists(ctx, file.StorageKey)
	if err != nil || !exists {
		return nil, fmt.Errorf("file not found in COS storage")
	}

	file.FileSize = fileSize
	file.Status = model.FileStatusCompleted
	if err := s.fileRepo.Update(file); err != nil {
		return nil, fmt.Errorf("failed to update file record: %w", err)
	}

	job, err := s.jobRepo.FindByUUID(jobUUID)
	if err == nil {
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

	return file, nil
}

func (s *UploadService) GetJob(ctx context.Context, uuid string) (*model.UploadJob, []model.UploadFile, error) {
	job, err := s.jobRepo.FindByUUID(uuid)
	if err != nil {
		return nil, nil, fmt.Errorf("upload job not found: %w", err)
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

func (s *UploadService) DeleteJob(ctx context.Context, uuid string) error {
	job, err := s.jobRepo.FindByUUID(uuid)
	if err != nil {
		return fmt.Errorf("upload job not found: %w", err)
	}

	files, err := s.fileRepo.FindByJobID(job.ID)
	if err != nil {
		return err
	}

	for _, file := range files {
		if job.Provider == model.UploadProviderLocal {
			os.Remove(file.StorageKey)
		} else if job.Provider == model.UploadProviderCOS && s.cosClient != nil {
			s.cosClient.DeleteObject(ctx, file.StorageKey)
		}
	}

	if err := s.fileRepo.DeleteByJobID(job.ID); err != nil {
		return err
	}

	return s.jobRepo.Delete(job.ID)
}

func (s *UploadService) GetJobFiles(ctx context.Context, jobID uint) ([]model.UploadFile, error) {
	return s.fileRepo.FindByJobID(jobID)
}

func (s *UploadService) GetFilePresignedGetURL(ctx context.Context, fileUUID string) (string, error) {
	if s.cosClient == nil {
		return "", fmt.Errorf("COS client not configured")
	}

	file, err := s.fileRepo.FindByUUID(fileUUID)
	if err != nil {
		return "", fmt.Errorf("upload file not found: %w", err)
	}

	return s.cosClient.GeneratePresignedGetURL(ctx, file.StorageKey)
}
