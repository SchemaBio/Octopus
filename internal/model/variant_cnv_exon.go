package model

// CNVExon represents a CNV exon-level result
type CNVExon struct {
	ID                  string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string             `json:"taskId" gorm:"size:36;index"`
	Gene                string             `json:"gene" gorm:"size:100;index"`
	Transcript          string             `json:"transcript" gorm:"size:100"`
	Exon                string             `json:"exon" gorm:"size:20"`
	Chromosome          string             `json:"chromosome" gorm:"size:10"`
	StartPosition       int64              `json:"startPosition"`
	EndPosition         int64              `json:"endPosition"`
	Type                string             `json:"type" gorm:"size:20"` // Amplification, Deletion
	CopyNumber          int                `json:"copyNumber"`
	Ratio               float64            `json:"ratio"`
	Confidence          float64            `json:"confidence"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (CNVExon) TableName() string {
	return "result_cnv_exons"
}

// CNVExonListQuery query parameters
type CNVExonListQuery struct {
	TaskID   string `form:"taskId" binding:"required"`
	Search   string `form:"search"`
	Gene     string `form:"gene"`
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
}
