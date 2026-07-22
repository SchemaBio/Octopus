package model

// STR represents a short tandem repeat result (23 columns from str.txt)
type STR struct {
	ID                  string   `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string   `json:"taskId" gorm:"size:36;index"`
	Chromosome          string   `json:"chromosome" gorm:"size:10"`
	Position            int64    `json:"position"`
	Gene                string   `json:"gene" gorm:"size:100;index"`
	RepeatUnit          string   `json:"repeatUnit" gorm:"size:50"`
	RefRepeats          int      `json:"refRepeats" gorm:"type:integer"`
	Allele1Repeats      string   `json:"allele1Repeats" gorm:"size:50"`
	Allele2Repeats      string   `json:"allele2Repeats" gorm:"size:50"`
	RepeatDisplay       string   `json:"repeatDisplay" gorm:"size:100"`
	Status              string   `json:"status" gorm:"size:30"` // Normal, Premutation, FullMutation
	Pathogenicity       string   `json:"pathogenicity" gorm:"size:50"`
	NormalRangeMax      int      `json:"normalRangeMax" gorm:"type:integer"`
	PathologicMin       int      `json:"pathologicMin" gorm:"type:integer"`
	Disease             string   `json:"disease" gorm:"size:200"`
	Inheritance         string   `json:"inheritance" gorm:"size:100"`
	HgncID              string   `json:"hgncId,omitempty" gorm:"size:50"`
	Depth               float64  `json:"depth" gorm:"type:numeric"`
	SpanningReads       string   `json:"spanningReads" gorm:"size:100"`
	FlankingReads       string   `json:"flankingReads" gorm:"size:100"`
	InRepeatReads       string   `json:"inRepeatReads" gorm:"size:100"`
	SwegenMean          *float64 `json:"swegenMean,omitempty" gorm:"type:numeric"`
	SwegenStd           *float64 `json:"swegenStd,omitempty" gorm:"type:numeric"`
	Source              string   `json:"source,omitempty" gorm:"size:200"`
	Filter              string   `json:"filter" gorm:"size:50"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (STR) TableName() string {
	return "result_strs"
}

// STRListQuery query parameters
type STRListQuery struct {
	TaskID   string `form:"taskId"`
	Search   string `form:"search"`
	Status   string `form:"status"`
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}
