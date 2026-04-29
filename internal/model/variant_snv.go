package model

// SNVIndel represents a SNV/Indel variant
type SNVIndel struct {
	ID                  string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string             `json:"taskId" gorm:"size:36;index"`
	Gene                string             `json:"gene" gorm:"size:100;index"`
	Chromosome          string             `json:"chromosome" gorm:"size:10"`
	Position            int64              `json:"position"`
	Ref                 string             `json:"ref" gorm:"size:1000"`
	Alt                 string             `json:"alt" gorm:"size:1000"`
	VariantType         string             `json:"variantType" gorm:"size:20"` // SNV, Insertion, Deletion, Complex
	Zygosity            string             `json:"zygosity" gorm:"size:20"`    // Heterozygous, Homozygous, Hemizygous
	AlleleFrequency     float64            `json:"alleleFrequency"`
	Depth               int                `json:"depth"`
	ACMGClassification  ACMGClassification `json:"acmgClassification" gorm:"size:30;index"`
	Transcript          string             `json:"transcript" gorm:"size:100"`
	HGVSc               string             `json:"hgvsc" gorm:"size:200"`
	HGVSp               string             `json:"hgvsp" gorm:"size:200"`
	Consequence         string             `json:"consequence" gorm:"size:200"`
	RsID                string             `json:"rsId,omitempty" gorm:"size:50"`
	ClinvarID           string             `json:"clinvarId,omitempty" gorm:"size:50"`
	ClinvarSignificance string             `json:"clinvarSignificance,omitempty" gorm:"size:200"`
	GnomadAF            *float64           `json:"gnomadAF,omitempty"`
	GnomadEasAF         *float64           `json:"gnomadEasAF,omitempty"`
	ExacAF              *float64           `json:"exacAF,omitempty"`
	SiftScore           *float64           `json:"siftScore,omitempty"`
	SiftPrediction      string             `json:"siftPrediction,omitempty" gorm:"size:50"`
	PolyphenScore       *float64           `json:"polyphenScore,omitempty"`
	PolyphenPrediction  string             `json:"polyphenPrediction,omitempty" gorm:"size:50"`
	CaddScore           *float64           `json:"caddScore,omitempty"`
	RevelScore          *float64           `json:"revelScore,omitempty"`
	SpliceAI            *float64           `json:"spliceAI,omitempty"`
	ACMGCriteria        string             `json:"acmgCriteria,omitempty" gorm:"type:text"`  // JSON array
	PubmedIDs           string             `json:"pubmedIds,omitempty" gorm:"type:text"`     // JSON array
	OmimID              string             `json:"omimId,omitempty" gorm:"size:50"`
	DiseaseAssociation  string             `json:"diseaseAssociation,omitempty" gorm:"type:text"`
	InheritanceMode     string             `json:"inheritanceMode,omitempty" gorm:"size:50"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (SNVIndel) TableName() string {
	return "result_snv_indels"
}

// SNVIndelListQuery query parameters
type SNVIndelListQuery struct {
	TaskID     string `form:"taskId" binding:"required"`
	Search     string `form:"search"`
	Gene       string `form:"gene"`
	Classification string `form:"classification"`
	GeneListID string `form:"geneListId"`
	Page       int    `form:"page" binding:"min=1"`
	PageSize   int    `form:"page_size" binding:"min=1,max=100"`
}
