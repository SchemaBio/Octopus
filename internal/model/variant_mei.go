package model

// MEIType represents the MEI type
type MEIType string

const (
	MEITypeLINE1   MEIType = "LINE1"
	MEITypeAlu     MEIType = "Alu"
	MEITypeSVA     MEIType = "SVA"
	MEITypeUnknown MEIType = "Unknown"
)

// MEIInsertionType represents the insertion type
type MEIInsertionType string

const (
	MEIInsertionTypeInsertion MEIInsertionType = "insertion"
	MEIInsertionTypeDeletion  MEIInsertionType = "deletion"
	MEIInsertionTypeComplex   MEIInsertionType = "complex"
)

// MEIVariant represents a mobile element insertion
type MEIVariant struct {
	ID                  string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string             `json:"taskId" gorm:"size:36;index"`
	Chromosome          string             `json:"chromosome" gorm:"size:10"`
	Position            int64              `json:"position"`
	MEIType             MEIType            `json:"meiType" gorm:"size:20"`
	InsertionType       MEIInsertionType   `json:"insertionType" gorm:"size:20"`
	Strand              string             `json:"strand" gorm:"size:5"` // + or -
	Length              int64              `json:"length"`
	Gene                string             `json:"gene" gorm:"size:100;index"`
	Transcript          string             `json:"transcript,omitempty" gorm:"size:100"`
	Impact              string             `json:"impact,omitempty" gorm:"size:50"`
	Zygosity            string             `json:"zygosity" gorm:"size:20"`
	SupportingReads     int                `json:"supportingReads"`
	TotalReads          int                `json:"totalReads"`
	Frequency           *float64           `json:"frequency,omitempty"`
	ACMGClassification  ACMGClassification `json:"acmgClassification,omitempty" gorm:"size:30"`
	ClinvarID           string             `json:"clinvarId,omitempty" gorm:"size:50"`
	DiseaseAssociation  string             `json:"diseaseAssociation,omitempty" gorm:"type:text"`
	Notes               string             `json:"notes,omitempty" gorm:"type:text"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (MEIVariant) TableName() string {
	return "result_meis"
}

// MEIListQuery query parameters
type MEIListQuery struct {
	TaskID   string `form:"taskId" binding:"required"`
	Search   string `form:"search"`
	MEIType  string `form:"meiType"`
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
}
