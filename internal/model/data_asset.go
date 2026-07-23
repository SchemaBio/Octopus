package model

import "time"

type DataAssetSource string

const (
	DataAssetSourceUpload  DataAssetSource = "upload"
	DataAssetSourceScanner DataAssetSource = "scanner"
)

// DataAsset is the organization-scoped identity for one stored data file.
// StorageKey is deliberately omitted from all browser response models.
type DataAsset struct {
	ID              uint            `json:"-" gorm:"primaryKey"`
	UUID            string          `json:"id" gorm:"uniqueIndex;size:36;not null"`
	ExternalOrgID   string          `json:"-" gorm:"size:100;index"`
	CreatedBy       uint            `json:"-" gorm:"index;not null"`
	UploadFileID    *uint           `json:"-" gorm:"uniqueIndex"`
	Provider        UploadProvider  `json:"provider" gorm:"size:20;index;uniqueIndex:idx_data_assets_provider_key;not null"`
	StorageKey      string          `json:"-" gorm:"size:1000;uniqueIndex:idx_data_assets_provider_key;not null"`
	FileName        string          `json:"file_name" gorm:"size:500;not null"`
	FileSize        int64           `json:"file_size" gorm:"default:0"`
	ReadType        ReadType        `json:"read_type" gorm:"size:20;index;not null"`
	ReferenceGenome string          `json:"reference_genome,omitempty" gorm:"size:20;index"`
	Status          FileStatus      `json:"status" gorm:"size:20;index;not null;default:pending"`
	Source          DataAssetSource `json:"source" gorm:"size:20;index;not null;default:upload"`
	ExpiresAt       *time.Time      `json:"expires_at,omitempty" gorm:"index;type:timestamptz"`
	CreatedAt       time.Time       `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt       time.Time       `json:"updated_at" gorm:"type:timestamptz"`
}

type DataAssetResponse struct {
	ID              string          `json:"id"`
	FileName        string          `json:"file_name"`
	FileSize        int64           `json:"file_size"`
	ReadType        ReadType        `json:"read_type"`
	ReferenceGenome string          `json:"reference_genome,omitempty"`
	Provider        UploadProvider  `json:"provider"`
	Status          FileStatus      `json:"status"`
	Source          DataAssetSource `json:"source"`
	ExpiresAt       *string         `json:"expires_at,omitempty"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

type DataAssetListQuery struct {
	Page            int        `form:"page" binding:"omitempty,min=1"`
	PageSize        int        `form:"page_size" binding:"omitempty,min=1,max=100"`
	Search          string     `form:"search"`
	Status          FileStatus `form:"status"`
	ReadType        ReadType   `form:"read_type"`
	ReferenceGenome string     `form:"reference_genome"`
	CreatedBy       uint       `json:"-"`
	OrgID           string     `json:"-"`
	IncludeAll      bool       `json:"-"`
}

type DataCenterConfigResponse struct {
	Provider        UploadProvider `json:"provider"`
	RetentionDays   int            `json:"retention_days"`
	Temporary       bool           `json:"temporary"`
	DownloadAllowed bool           `json:"download_allowed"`
}

func DataAssetToResponse(asset *DataAsset) DataAssetResponse {
	response := DataAssetResponse{
		ID: asset.UUID, FileName: asset.FileName, FileSize: asset.FileSize,
		ReadType: asset.ReadType, Provider: asset.Provider, Status: asset.Status,
		ReferenceGenome: asset.ReferenceGenome,
		Source:          asset.Source, CreatedAt: asset.CreatedAt.Format(time.RFC3339),
		UpdatedAt: asset.UpdatedAt.Format(time.RFC3339),
	}
	if asset.ExpiresAt != nil {
		value := asset.ExpiresAt.Format(time.RFC3339)
		response.ExpiresAt = &value
	}
	return response
}

func (DataAsset) TableName() string { return "data_assets" }
