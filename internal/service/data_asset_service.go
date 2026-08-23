package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"gorm.io/gorm"
)

type DataAssetService struct {
	cfg   *config.Config
	repo  *repository.DataAssetRepository
	files *repository.UploadFileRepository
}

type DataAssetDownload struct{ LocalPath, URL string }

var ErrDataAssetDownloadDisabled = errors.New("data downloads are disabled in SaaS mode")

func NewDataAssetService(cfg *config.Config) *DataAssetService {
	return &DataAssetService{cfg: cfg, repo: repository.NewDataAssetRepository(), files: repository.NewUploadFileRepository()}
}

func (s *DataAssetService) Config() model.DataCenterConfigResponse {
	temporary := s.cfg.Storage.RetentionDays > 0
	var maxFileSizeBytes int64
	if temporary {
		maxFileSizeBytes = model.SaaSMaxUploadFileBytes
	}
	return model.DataCenterConfigResponse{
		Provider:         model.UploadProvider(s.cfg.Storage.Provider),
		RetentionDays:    s.cfg.Storage.RetentionDays,
		Temporary:        temporary,
		DownloadAllowed:  !temporary,
		MaxFileSizeBytes: maxFileSizeBytes,
	}
}

func (s *DataAssetService) List(query *model.DataAssetListQuery) ([]model.DataAssetResponse, int64, error) {
	assets, total, err := s.repo.Paginate(query)
	if err != nil {
		return nil, 0, err
	}
	items := make([]model.DataAssetResponse, len(assets))
	for i := range assets {
		items[i] = model.DataAssetToResponse(&assets[i])
	}
	return items, total, nil
}

func (s *DataAssetService) Get(uuid string, actor model.OverlayActor) (*model.DataAsset, error) {
	return s.repo.FindScopedByUUID(uuid, actor)
}

func (s *DataAssetService) Update(uuid string, req *model.DataAssetUpdateRequest, actor model.OverlayActor) (*model.DataAsset, error) {
	asset, err := s.Get(uuid, actor)
	if err != nil || asset.Status == model.FileStatusDeleted {
		return nil, fmt.Errorf("data asset not found")
	}
	if actor.Role != string(model.SystemRoleSuperAdmin) && asset.CreatedBy != actor.UserID {
		return nil, fmt.Errorf("data asset not found")
	}
	asset.InternalID = strings.TrimSpace(req.InternalID)
	if err := s.repo.Update(asset); err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *DataAssetService) Download(ctx context.Context, uuid string, actor model.OverlayActor) (*DataAssetDownload, string, error) {
	if s.cfg.Storage.RetentionDays > 0 {
		return nil, "", ErrDataAssetDownloadDisabled
	}
	asset, err := s.Get(uuid, actor)
	if err != nil || asset.Status != model.FileStatusCompleted {
		return nil, "", fmt.Errorf("data asset not found")
	}
	if asset.Provider == model.UploadProviderS3 {
		storage, err := newS3Storage(ctx, s.cfg.Storage)
		if err != nil {
			return nil, "", err
		}
		url, err := storage.presignDownload(ctx, asset.StorageKey, asset.FileName)
		return &DataAssetDownload{URL: url}, asset.FileName, err
	}
	path, err := safeLocalUploadPath(s.cfg.Storage.LocalDir, asset.StorageKey)
	if err != nil {
		return nil, "", err
	}
	return &DataAssetDownload{LocalPath: path}, asset.FileName, nil
}

func (s *DataAssetService) Delete(ctx context.Context, uuid string, actor model.OverlayActor) error {
	asset, err := s.Get(uuid, actor)
	if err != nil {
		return fmt.Errorf("data asset not found")
	}
	if actor.Role != string(model.SystemRoleSuperAdmin) && asset.CreatedBy != actor.UserID {
		return fmt.Errorf("data asset not found")
	}
	if err := s.deleteStoredObject(ctx, asset); err != nil {
		return err
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := clearSampleAssetLinks(tx, asset.ID); err != nil {
			return err
		}
		asset.Status = model.FileStatusDeleted
		if err := tx.Save(asset).Error; err != nil {
			return err
		}
		if asset.UploadFileID != nil {
			_ = tx.Model(&model.UploadFile{}).Where("id = ?", *asset.UploadFileID).Update("status", model.FileStatusDeleted).Error
		}
		return nil
	})
}

// clearSampleAssetLinks removes both manual and automatic links to an asset and
// returns affected samples to the ordinary unmatched state.
func clearSampleAssetLinks(tx *gorm.DB, assetID uint) error {
	sampleIDs := tx.Model(&model.SampleDataLink{}).
		Select("sample_id").Where("read1_asset_id = ? OR read2_asset_id = ?", assetID, assetID)
	if err := tx.Model(&model.Sample{}).Where("id IN (?)", sampleIDs).Updates(map[string]interface{}{
		"matched_pair": "null", "manual_matched_pair": "null", "auto_matched_pair": "null",
		"match_status": model.SampleMatchUnmatched, "match_mode": "",
	}).Error; err != nil {
		return err
	}
	return tx.Where("read1_asset_id = ? OR read2_asset_id = ?", assetID, assetID).Delete(&model.SampleDataLink{}).Error
}

func (s *DataAssetService) deleteStoredObject(ctx context.Context, asset *model.DataAsset) error {
	if asset.Provider == model.UploadProviderS3 {
		storage, err := newS3Storage(ctx, s.cfg.Storage)
		if err != nil {
			return err
		}
		return storage.delete(ctx, asset.StorageKey)
	}
	path, err := safeLocalUploadPath(s.cfg.Storage.LocalDir, asset.StorageKey)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *DataAssetService) StartRetentionCleanup(ctx context.Context, interval time.Duration) {
	if s.cfg.Storage.RetentionDays <= 0 {
		return
	}
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		s.cleanupExpired(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanupExpired(ctx)
			}
		}
	}()
}

func (s *DataAssetService) cleanupExpired(ctx context.Context) {
	for {
		assets, err := s.repo.FindExpired(time.Now(), 100)
		if err != nil || len(assets) == 0 {
			return
		}
		for i := range assets {
			if err := s.deleteStoredObject(ctx, &assets[i]); err != nil {
				continue
			}
			_ = database.GetDB().Transaction(func(tx *gorm.DB) error {
				if err := clearSampleAssetLinks(tx, assets[i].ID); err != nil {
					return err
				}
				assets[i].Status = model.FileStatusDeleted
				if err := tx.Save(&assets[i]).Error; err != nil {
					return err
				}
				if assets[i].UploadFileID != nil {
					return tx.Model(&model.UploadFile{}).Where("id = ?", *assets[i].UploadFileID).Update("status", model.FileStatusDeleted).Error
				}
				return nil
			})
		}
		if len(assets) < 100 {
			return
		}
	}
}
