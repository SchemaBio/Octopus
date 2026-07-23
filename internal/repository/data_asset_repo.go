package repository

import (
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

type DataAssetRepository struct {
	*Repository[model.DataAsset]
}

func NewDataAssetRepository() *DataAssetRepository {
	return &DataAssetRepository{Repository: NewRepository[model.DataAsset]()}
}

func (r *DataAssetRepository) FindByUploadFileID(uploadFileID uint) (*model.DataAsset, error) {
	return r.FindOneByCondition(map[string]interface{}{"upload_file_id": uploadFileID})
}

func (r *DataAssetRepository) ExistsByProviderKey(provider model.UploadProvider, storageKey string) bool {
	var count int64
	r.db.Model(&model.DataAsset{}).Where("provider = ? AND storage_key = ?", provider, storageKey).Count(&count)
	return count > 0
}

func (r *DataAssetRepository) FindScopedByUUID(uuid string, actor model.OverlayActor) (*model.DataAsset, error) {
	db := r.db.Where("uuid = ?", uuid)
	if actor.Role != string(model.SystemRoleSuperAdmin) {
		if actor.OrgID != "" {
			db = db.Where("external_org_id = ?", actor.OrgID)
		} else {
			db = db.Where("external_org_id = '' AND created_by = ?", actor.UserID)
		}
	}
	var asset model.DataAsset
	if err := db.First(&asset).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *DataAssetRepository) Paginate(query *model.DataAssetListQuery) ([]model.DataAsset, int64, error) {
	db := r.db.Model(&model.DataAsset{}).Where("status <> ?", model.FileStatusDeleted)
	if !query.IncludeAll {
		if query.OrgID != "" {
			db = db.Where("external_org_id = ?", query.OrgID)
		} else {
			db = db.Where("external_org_id = '' AND created_by = ?", query.CreatedBy)
		}
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		db = db.Where("file_name ILIKE ? OR uuid ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.ReadType != "" {
		db = db.Where("read_type = ?", query.ReadType)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := query.Page, query.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var assets []model.DataAsset
	err := db.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&assets).Error
	return assets, total, err
}

func (r *DataAssetRepository) FindExpired(now time.Time, limit int) ([]model.DataAsset, error) {
	if limit <= 0 {
		limit = 100
	}
	var assets []model.DataAsset
	err := r.db.Where("expires_at IS NOT NULL AND expires_at <= ? AND status <> ?", now, model.FileStatusDeleted).
		Order("expires_at ASC").Limit(limit).Find(&assets).Error
	return assets, err
}

func (r *DataAssetRepository) FindCompletedByScope(orgID string, createdBy uint) ([]model.DataAsset, error) {
	db := r.db.Where("status = ?", model.FileStatusCompleted)
	if orgID != "" {
		db = db.Where("external_org_id = ?", orgID)
	} else {
		db = db.Where("external_org_id = '' AND created_by = ?", createdBy)
	}
	var assets []model.DataAsset
	err := db.Order("created_at DESC").Find(&assets).Error
	return assets, err
}
