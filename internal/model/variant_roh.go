package model

// ROHRegion represents a region of homozygosity (8 columns from roh.anno.txt)
type ROHRegion struct {
	ID                      string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID                  string             `json:"taskId" gorm:"size:36;index"`
	Chr                     string             `json:"chr" gorm:"size:10"`
	Begin                   int64              `json:"begin"`
	End                     int64              `json:"end"`
	SizeMb                  float64            `json:"sizeMb" gorm:"type:numeric"`
	NbVariants              int                `json:"nbVariants" gorm:"type:integer"`
	PercentageHomozygosity  float64            `json:"percentageHomozygosity" gorm:"type:numeric"`
	RecessiveGenes          string             `json:"recessiveGenes,omitempty" gorm:"type:text"`
	GeneCount               int                `json:"geneCount" gorm:"type:integer"`
	VariantReviewStatus     `json:"reviewStatus" gorm:"embedded"`
}

func (ROHRegion) TableName() string {
	return "result_roh_regions"
}

// ROHListQuery query parameters
type ROHListQuery struct {
	TaskID   string `form:"taskId" binding:"required"`
	Search   string `form:"search"`
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
}
