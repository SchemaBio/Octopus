package service

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrResultPackageUnsupported = errors.New("external report result packages require COS/S3 storage")
	ErrResultPackageNotReady    = errors.New("RESULT_PACKAGE_NOT_READY")
)

const resultPackageBuildStaleAfter = 15 * time.Minute

type resultPackageSource struct {
	key  string
	rel  string
	size int64
	mod  time.Time
}

// ResultPackageService creates and serves cached ZIPs for one task execution.
type ResultPackageService struct {
	cfg  *config.Config
	repo *repository.ResultPackageRepository
}

func NewResultPackageService(cfg *config.Config) *ResultPackageService {
	return &ResultPackageService{cfg: cfg, repo: repository.NewResultPackageRepository()}
}

func (s *ResultPackageService) Prepare(ctx context.Context, task *model.Task) (*model.ResultPackageResponse, error) {
	if err := validateResultPackageTask(s.cfg, task); err != nil {
		return nil, err
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	prefix := resultPackagePrefix(task)
	sources, fingerprint, err := s.collectSources(ctx, storage, prefix)
	if err != nil {
		// Keep a failed row so the user can retry after the archive is fixed.
		claimed, _, claimErr := s.repo.Claim(task.UUID, task.ExecutionAttemptID, task.ExternalOrgID, task.CreatedBy, fingerprint, resultPackageBuildStaleAfter)
		if claimErr != nil {
			return nil, claimErr
		}
		if claimed != nil {
			if markErr := s.repo.MarkFailed(claimed.ID, err.Error()); markErr != nil {
				return nil, markErr
			}
		}
		item, findErr := s.repo.FindByTaskAttempt(task.UUID, task.ExecutionAttemptID)
		if findErr != nil {
			return nil, findErr
		}
		return s.statusResponse(ctx, storage, task, item, false)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("result archive contains no parquet files")
	}
	claimed, shouldBuild, err := s.repo.Claim(task.UUID, task.ExecutionAttemptID, task.ExternalOrgID, task.CreatedBy, fingerprint, resultPackageBuildStaleAfter)
	if err != nil {
		return nil, err
	}
	if claimed == nil {
		return nil, fmt.Errorf("result package state is unavailable")
	}
	if claimed.Status == model.ResultPackageReady && !shouldBuild {
		if _, statErr := storage.stat(ctx, claimed.ObjectKey); statErr != nil {
			_ = s.repo.MarkFailed(claimed.ID, "cached result package is missing")
			claimed, shouldBuild, err = s.repo.Claim(task.UUID, task.ExecutionAttemptID, task.ExternalOrgID, task.CreatedBy, fingerprint, resultPackageBuildStaleAfter)
			if err != nil {
				return nil, err
			}
		}
	}
	if shouldBuild {
		go s.build(context.Background(), storage, task.UUID, task.ExecutionAttemptID, prefix, claimed, sources, fingerprint)
	}
	return s.statusResponse(ctx, storage, task, claimed, true)
}

func (s *ResultPackageService) Status(ctx context.Context, task *model.Task) (*model.ResultPackageResponse, error) {
	if err := validateResultPackageTask(s.cfg, task); err != nil {
		return nil, err
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.FindByTaskAttempt(task.UUID, task.ExecutionAttemptID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return &model.ResultPackageResponse{TaskUUID: task.UUID, ExecutionAttemptID: task.ExecutionAttemptID, Status: model.ResultPackagePending}, nil
	}
	return s.statusResponse(ctx, storage, task, item, false)
}

func validateResultPackageTask(cfg *config.Config, task *model.Task) error {
	if cfg == nil || cfg.Storage.Provider != "s3" {
		return ErrResultPackageUnsupported
	}
	if task == nil {
		return fmt.Errorf("task not found")
	}
	if _, err := uuid.Parse(task.UUID); err != nil {
		return fmt.Errorf("valid task UUID is required")
	}
	if _, err := uuid.Parse(task.ExecutionAttemptID); err != nil {
		return fmt.Errorf("valid execution attempt ID is required")
	}
	if _, err := uuid.Parse(task.ExternalOrgID); err != nil {
		return fmt.Errorf("valid organization ID is required")
	}
	return nil
}

func resultPackagePrefix(task *model.Task) string {
	return path.Join("organizations", task.ExternalOrgID, "workflows", task.UUID, "attempts", task.ExecutionAttemptID)
}

func (s *ResultPackageService) collectSources(ctx context.Context, storage *s3Storage, prefix string) ([]resultPackageSource, string, error) {
	objects, err := storage.list(ctx, prefix+"/")
	if err != nil {
		return nil, "", err
	}
	var sources []resultPackageSource
	var total int64
	manifest := false
	for _, object := range objects {
		rel, err := safeResultPackageRelativePath(prefix, object.Key)
		if err != nil {
			return nil, "", err
		}
		lower := strings.ToLower(rel)
		if strings.HasSuffix(lower, ".parquet") {
			sources = append(sources, resultPackageSource{key: object.Key, rel: rel, size: object.Size, mod: object.LastModified})
		} else if path.Base(lower) == "outputs.resolved.json" {
			manifest = true
			sources = append(sources, resultPackageSource{key: object.Key, rel: rel, size: object.Size, mod: object.LastModified})
		}
	}
	if !manifest {
		return nil, "", fmt.Errorf("result archive is missing outputs.resolved.json")
	}
	if len(sources) < 2 {
		return nil, "", fmt.Errorf("result archive contains no parquet files")
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].rel < sources[j].rel })
	h := sha256.New()
	for _, source := range sources {
		if source.size < 0 || total > int64(^uint64(0)>>1)-source.size {
			return nil, "", fmt.Errorf("result archive size is invalid")
		}
		total += source.size
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", source.rel, source.size, source.mod.UnixNano())
	}
	maxBytes := int64(s.cfg.Report.PackageMaxSizeMB) * 1024 * 1024
	if maxBytes > 0 && total > maxBytes {
		return nil, "", fmt.Errorf("result archive exceeds maximum source size of %d MB", s.cfg.Report.PackageMaxSizeMB)
	}
	return sources, hex.EncodeToString(h.Sum(nil)), nil
}

func safeResultPackageRelativePath(prefix, key string) (string, error) {
	prefix = strings.Trim(prefix, "/")
	key = strings.ReplaceAll(key, `\`, "/")
	if !strings.HasPrefix(key, prefix+"/") {
		return "", fmt.Errorf("result object is outside execution prefix")
	}
	rawRel := strings.TrimPrefix(key, prefix+"/")
	for _, segment := range strings.Split(rawRel, "/") {
		if segment == ".." {
			return "", fmt.Errorf("result object path escapes execution prefix")
		}
	}
	rel := path.Clean(rawRel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\x00") {
		return "", fmt.Errorf("result object path escapes execution prefix")
	}
	return rel, nil
}

func (s *ResultPackageService) build(ctx context.Context, storage *s3Storage, taskUUID, attemptID, prefix string, item *model.ResultPackage, sources []resultPackageSource, fingerprint string) {
	tmp, err := os.CreateTemp("", "octopus-result-package-*.zip")
	if err != nil {
		_ = s.repo.MarkFailedForFingerprint(item.ID, fingerprint, err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	zw := zip.NewWriter(tmp)
	for _, source := range sources {
		reader, openErr := storage.open(ctx, source.key)
		if openErr != nil {
			_ = zw.Close()
			_ = tmp.Close()
			_ = s.repo.MarkFailedForFingerprint(item.ID, fingerprint, fmt.Sprintf("failed to read result object: %v", openErr))
			return
		}
		header := &zip.FileHeader{Name: source.rel, Method: zip.Deflate}
		header.SetModTime(source.mod)
		writer, createErr := zw.CreateHeader(header)
		if createErr == nil {
			_, createErr = io.Copy(writer, reader)
		}
		reader.Close()
		if createErr != nil {
			_ = zw.Close()
			_ = tmp.Close()
			_ = s.repo.MarkFailedForFingerprint(item.ID, fingerprint, fmt.Sprintf("failed to create result package: %v", createErr))
			return
		}
	}
	if err := zw.Close(); err != nil {
		_ = tmp.Close()
		_ = s.repo.MarkFailedForFingerprint(item.ID, fingerprint, fmt.Sprintf("failed to finalize result package: %v", err))
		return
	}
	if err := tmp.Close(); err != nil {
		_ = s.repo.MarkFailedForFingerprint(item.ID, fingerprint, fmt.Sprintf("failed to close result package: %v", err))
		return
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		_ = s.repo.MarkFailedForFingerprint(item.ID, fingerprint, err.Error())
		return
	}
	objectKey := path.Join(prefix, "_result-packages", fingerprint+".zip")
	file, err := os.Open(tmpPath)
	if err != nil {
		_ = s.repo.MarkFailedForFingerprint(item.ID, fingerprint, err.Error())
		return
	}
	uploadErr := storage.putReader(ctx, objectKey, "application/zip", file, info.Size())
	file.Close()
	if uploadErr != nil {
		_ = s.repo.MarkFailedForFingerprint(item.ID, fingerprint, fmt.Sprintf("failed to upload result package: %v", uploadErr))
		return
	}
	if err := s.repo.MarkReady(item.ID, objectKey, fingerprint, info.Size()); err != nil {
		return
	}
}

func (s *ResultPackageService) statusResponse(ctx context.Context, storage *s3Storage, task *model.Task, item *model.ResultPackage, _ bool) (*model.ResultPackageResponse, error) {
	response := &model.ResultPackageResponse{TaskUUID: task.UUID, ExecutionAttemptID: task.ExecutionAttemptID, Status: model.ResultPackagePending}
	if item == nil {
		return response, nil
	}
	response.Status = item.Status
	response.SizeBytes = item.SizeBytes
	response.SourceFingerprint = item.SourceFingerprint
	response.Error = item.Error
	response.StartedAt = item.StartedAt
	response.FinishedAt = item.FinishedAt
	if item.Status != model.ResultPackageReady {
		return response, nil
	}
	if _, err := storage.stat(ctx, item.ObjectKey); err != nil {
		response.Status = model.ResultPackageFailed
		response.Error = "cached result package is missing"
		return response, nil
	}
	name := fmt.Sprintf("task-%s-results.zip", task.UUID)
	url, err := storage.presignDownload(ctx, item.ObjectKey, name)
	if err != nil {
		return nil, err
	}
	expires := time.Now().UTC().Add(storage.expiry)
	response.ResultPackageURL = url
	response.FileName = name
	response.ExpiresAt = &expires
	return response, nil
}
