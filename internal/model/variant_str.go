package model

// STRStatus represents the STR severity status
type STRStatus string

const (
	STRStatusNormal      STRStatus = "Normal"
	STRStatusPremutation STRStatus = "Premutation"
	STRStatusFullMutation STRStatus = "FullMutation"
)

// STR represents a short tandem repeat result
type STR struct {
	ID                  string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string             `json:"taskId" gorm:"size:36;index"`
	Gene                string             `json:"gene" gorm:"size:100;index"`
	Transcript          string             `json:"transcript" gorm:"size:100"`
	Locus               string             `json:"locus" gorm:"size:100"`
	RepeatUnit          string             `json:"repeatUnit" gorm:"size:50"`
	RepeatCount         int                `json:"repeatCount"`
	NormalRangeMin      int                `json:"normalRangeMin"`
	NormalRangeMax      int                `json:"normalRangeMax"`
	Status              STRStatus          `json:"status" gorm:"size:20"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (STR) TableName() string {
	return "result_strs"
}

// STRListQuery query parameters
type STRListQuery struct {
	TaskID   string `form:"taskId" binding:"required"`
	Search   string `form:"search"`
	Status   string `form:"status"`
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
}
