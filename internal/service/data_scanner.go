package service

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
)

// DataScanner imports stable FASTQ files from one explicitly configured owner
// scope. WalkDir never follows symlinks, so a mounted inbox cannot escape its
// configured root through a link.
type DataScanner struct {
	cfg    *config.Config
	assets *repository.DataAssetRepository
}

func NewDataScanner(cfg *config.Config) *DataScanner {
	return &DataScanner{cfg: cfg, assets: repository.NewDataAssetRepository()}
}

func (s *DataScanner) Enabled() bool {
	return (s.cfg.Storage.Provider == "local" && s.cfg.Storage.ScanLocalDir != "") ||
		(s.cfg.Storage.Provider == "s3" && s.cfg.Storage.S3ScanPrefix != "")
}

func (s *DataScanner) Start(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	interval := s.cfg.Storage.ScanInterval
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		s.scan(ctx, interval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.scan(ctx, interval)
			}
		}
	}()
}

func (s *DataScanner) scan(ctx context.Context, stableFor time.Duration) {
	if s.cfg.Storage.Provider == "s3" {
		s.scanS3(ctx, stableFor)
		return
	}
	s.scanLocal(stableFor)
}

func (s *DataScanner) scanLocal(stableFor time.Duration) {
	root, err := filepath.Abs(s.cfg.Storage.ScanLocalDir)
	if err != nil {
		return
	}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, absolute)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) < stableFor {
			return nil
		}
		s.register(model.UploadProviderLocal, absolute, entry.Name(), info.Size(), info.ModTime())
		return nil
	})
}

func (s *DataScanner) scanS3(ctx context.Context, stableFor time.Duration) {
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return
	}
	objects, err := storage.list(ctx, s.cfg.Storage.S3ScanPrefix)
	if err != nil {
		return
	}
	for _, object := range objects {
		if !object.LastModified.IsZero() && time.Since(object.LastModified) < stableFor {
			continue
		}
		s.register(model.UploadProviderS3, object.Key, filepath.Base(object.Key), object.Size, object.LastModified)
	}
}

func (s *DataScanner) register(provider model.UploadProvider, storageKey, filename string, size int64, discoveredAt time.Time) {
	_, readType, ok := parseFASTQPairName(filename)
	if !ok || s.assets.ExistsByProviderKey(provider, storageKey) {
		return
	}
	var expiresAt *time.Time
	if s.cfg.Storage.RetentionDays > 0 {
		base := discoveredAt
		if base.IsZero() {
			base = time.Now()
		}
		value := base.AddDate(0, 0, s.cfg.Storage.RetentionDays)
		expiresAt = &value
	}
	_ = s.assets.Create(&model.DataAsset{
		UUID: uuid.NewString(), ExternalOrgID: s.cfg.Storage.ScanOrgID, CreatedBy: uint(s.cfg.Storage.ScanUserID),
		Provider: provider, StorageKey: storageKey, FileName: filename, FileSize: size,
		ReadType: readType, Status: model.FileStatusCompleted, Source: model.DataAssetSourceScanner,
		ExpiresAt: expiresAt,
	})
}
