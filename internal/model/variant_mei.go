package model

// MEIVariant represents a mobile element insertion (21 columns from mei.txt)
type MEIVariant struct {
	ID                  string   `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string   `json:"taskId" gorm:"size:36;index"`
	Chromosome          string   `json:"chromosome" gorm:"size:10"`
	Position            int64    `json:"position"`
	MEIID               string   `json:"meiId,omitempty" gorm:"size:100"`
	TEType              string   `json:"teType" gorm:"size:20"`   // SVA, L1
	TEFamily            string   `json:"teFamily" gorm:"size:50"` // SVA_F, L1ME3Cz, etc.
	Direction           string   `json:"direction" gorm:"size:5"` // 5' or 3'
	Confidence          string   `json:"confidence" gorm:"size:20"`
	SupportingReads     int      `json:"supportingReads" gorm:"type:integer"`
	AvgSoftClipLength   float64  `json:"avgSoftClipLength" gorm:"type:numeric"`
	Gene                string   `json:"gene" gorm:"size:100;index"`
	Transcript          string   `json:"transcript,omitempty" gorm:"size:100"`
	Location            string   `json:"location,omitempty" gorm:"size:200"`
	Consequence         string   `json:"consequence,omitempty" gorm:"size:200"`
	Impact              string   `json:"impact,omitempty" gorm:"size:20"`
	Cytoband            string   `json:"cytoband,omitempty" gorm:"size:50"`
	ClinvarSig          string   `json:"clinvarSig,omitempty" gorm:"size:500"`
	ClinvarDN           string   `json:"clinvarDn,omitempty" gorm:"size:500"`
	ClinvarStar         string   `json:"clinvarStar,omitempty" gorm:"size:50"`
	GnomadAF            *float64 `json:"gnomadAF,omitempty" gorm:"type:numeric"`
	HgncID              string   `json:"hgncId,omitempty" gorm:"size:50"`
	Filter              string   `json:"filter" gorm:"size:50"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (MEIVariant) TableName() string {
	return "result_mei_variants"
}

// MEIListQuery query parameters
type MEIListQuery struct {
	TaskID   string `form:"taskId"`
	Search   string `form:"search"`
	TEType   string `form:"teType"`
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}
