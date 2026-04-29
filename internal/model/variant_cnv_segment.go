package model

// CNVSegment represents a CNV segment
type CNVSegment struct {
	ID                  string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string             `json:"taskId" gorm:"size:36;index"`
	Chromosome          string             `json:"chromosome" gorm:"size:10"`
	StartPosition       int64              `json:"startPosition"`
	EndPosition         int64              `json:"endPosition"`
	Length              int64              `json:"length"`
	Type                string             `json:"type" gorm:"size:20"` // Amplification, Deletion
	CopyNumber          int                `json:"copyNumber"`
	Genes               string             `json:"genes" gorm:"type:text"` // JSON array
	Confidence          float64            `json:"confidence"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (CNVSegment) TableName() string {
	return "result_cnv_segments"
}

// CNVSegmentListQuery query parameters
type CNVSegmentListQuery struct {
	TaskID   string `form:"taskId" binding:"required"`
	Search   string `form:"search"`
	Type     string `form:"type"`
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
}
