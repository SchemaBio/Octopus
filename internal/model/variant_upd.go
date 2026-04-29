package model

// UPDType represents the UPD type
type UPDType string

const (
	UPDTypeIsodisomy    UPDType = "Isodisomy"
	UPDTypeHeterodisomy UPDType = "Heterodisomy"
)

// ParentOfOrigin represents the parent of origin
type ParentOfOrigin string

const (
	ParentOfOriginMaternal ParentOfOrigin = "Maternal"
	ParentOfOriginPaternal ParentOfOrigin = "Paternal"
	ParentOfOriginUnknown  ParentOfOrigin = "Unknown"
)

// UPDRegion represents a UPD region
type UPDRegion struct {
	ID                  string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string             `json:"taskId" gorm:"size:36;index"`
	Chromosome          string             `json:"chromosome" gorm:"size:10"`
	StartPosition       int64              `json:"startPosition"`
	EndPosition         int64              `json:"endPosition"`
	Length              int64              `json:"length"`
	Type                UPDType            `json:"type" gorm:"size:20"`
	Genes               string             `json:"genes" gorm:"type:text"` // JSON array
	ParentOfOrigin      ParentOfOrigin     `json:"parentOfOrigin,omitempty" gorm:"size:20"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (UPDRegion) TableName() string {
	return "result_upd_regions"
}

// UPDListQuery query parameters
type UPDListQuery struct {
	TaskID   string `form:"taskId" binding:"required"`
	Search   string `form:"search"`
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
}
