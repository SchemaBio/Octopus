package model

// MitochondrialPathogenicity represents MT variant pathogenicity
type MitochondrialPathogenicity string

const (
	MTPathogenic       MitochondrialPathogenicity = "Pathogenic"
	MTLikelyPathogenic MitochondrialPathogenicity = "Likely_Pathogenic"
	MTVUS              MitochondrialPathogenicity = "VUS"
	MTLikelyBenign     MitochondrialPathogenicity = "Likely_Benign"
	MTBenign           MitochondrialPathogenicity = "Benign"
)

// MitochondrialVariant represents a mitochondrial variant
type MitochondrialVariant struct {
	ID                  string                  `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string                  `json:"taskId" gorm:"size:36;index"`
	Position            int64                   `json:"position"`
	Ref                 string                  `json:"ref" gorm:"size:1000"`
	Alt                 string                  `json:"alt" gorm:"size:1000"`
	Gene                string                  `json:"gene" gorm:"size:100;index"`
	Heteroplasmy        float64                 `json:"heteroplasmy"`
	Pathogenicity       MitochondrialPathogenicity `json:"pathogenicity" gorm:"size:30"`
	AssociatedDisease   string                  `json:"associatedDisease" gorm:"type:text"`
	Haplogroup          string                  `json:"haplogroup,omitempty" gorm:"size:50"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (MitochondrialVariant) TableName() string {
	return "result_mt_variants"
}

// MTListQuery query parameters
type MTListQuery struct {
	TaskID   string `form:"taskId" binding:"required"`
	Search   string `form:"search"`
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
}
