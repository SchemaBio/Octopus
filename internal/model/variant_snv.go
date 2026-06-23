package model

// SNVIndel represents a SNV/Indel variant
type SNVIndel struct {
	ID                  string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string             `json:"taskId" gorm:"size:36;index"`
	Chromosome          string             `json:"chromosome" gorm:"size:10"`
	Position            int64              `json:"position"`
	VariantID           string             `json:"variantId,omitempty" gorm:"size:100"`
	Ref                 string             `json:"ref" gorm:"size:1000"`
	Alt                 string             `json:"alt" gorm:"size:1000"`
	VariantType         string             `json:"variantType" gorm:"size:20"` // SNP, DEL, INS
	Quality             float64            `json:"quality" gorm:"type:numeric"`
	Filter              string             `json:"filter" gorm:"size:50"`
	Genotype            string             `json:"genotype" gorm:"size:20"`  // 0/1, 1/1, 0|1
	Zygosity            string             `json:"zygosity" gorm:"size:20"`  // Hom, Het
	PhaseSet            string             `json:"phaseSet,omitempty" gorm:"size:50"`
	Depth               int                `json:"depth" gorm:"type:integer"`
	AD                  string             `json:"ad,omitempty" gorm:"size:50"` // comma-separated, e.g. "0,77"
	VAF                 float64            `json:"vaf" gorm:"type:numeric"`
	Gene                string             `json:"gene" gorm:"size:100;index"`
	Transcript          string             `json:"transcript" gorm:"size:100"`
	Location            string             `json:"location,omitempty" gorm:"size:200"`
	Consequence         string             `json:"consequence" gorm:"size:200"`
	Impact              string             `json:"impact,omitempty" gorm:"size:20"` // HIGH, MODERATE, LOW, MODIFIER
	HGVSc               string             `json:"hgvsc" gorm:"size:200"`
	HGVSp               string             `json:"hgvsp" gorm:"size:200"`
	AminoAcids          string             `json:"aminoAcids,omitempty" gorm:"size:50"`
	Cytoband            string             `json:"cytoband,omitempty" gorm:"size:50"`
	ClinvarSignificance string             `json:"clinvarSignificance,omitempty" gorm:"size:500"`
	ClinvarRevStat      string             `json:"clinvarRevStat,omitempty" gorm:"size:200"`
	ClinvarDN           string             `json:"clinvarDn,omitempty" gorm:"size:500"`
	ClinvarStar         string             `json:"clinvarStar,omitempty" gorm:"size:50"`
	GnomadAF            *float64           `json:"gnomadAF,omitempty" gorm:"type:numeric"`
	GnomadEasAF         *float64           `json:"gnomadEasAF,omitempty" gorm:"type:numeric"`
	GnomadNhomaltXX     *float64           `json:"gnomadNhomaltXX,omitempty" gorm:"type:numeric"`
	GnomadNhomaltXY     *float64           `json:"gnomadNhomaltXY,omitempty" gorm:"type:numeric"`
	PangolinGain        *float64           `json:"pangolinGain,omitempty" gorm:"type:numeric"`
	PangolinLoss        *float64           `json:"pangolinLoss,omitempty" gorm:"type:numeric"`
	PangolinAN          *float64           `json:"pangolinAN,omitempty" gorm:"type:numeric"`
	EVOScore            *float64           `json:"evoScore,omitempty" gorm:"type:numeric"`
	EVOScoreAN          *float64           `json:"evoScoreAN,omitempty" gorm:"type:numeric"`
	AlphaMissenseAM     *float64           `json:"alphaMissenseAM,omitempty" gorm:"type:numeric"`
	AlphaMissenseAMC    string             `json:"alphaMissenseAMC,omitempty" gorm:"size:20"`
	HgncID              string             `json:"hgncId,omitempty" gorm:"size:50"`
	RsID                string             `json:"rsId,omitempty" gorm:"size:200"`
	MaxAF               *float64           `json:"maxAF,omitempty" gorm:"type:numeric"`
	GenccMoi            string             `json:"genccMoi,omitempty" gorm:"size:200"`
	GenccDiseaseTitle   string             `json:"genccDiseaseTitle,omitempty" gorm:"type:text"`
	GenccMoiTitle       string             `json:"genccMoiTitle,omitempty" gorm:"type:text"`
	ACMGClassification  ACMGClassification `json:"acmgClassification" gorm:"size:30;index"`
	DiseaseAssociation  string             `json:"diseaseAssociation,omitempty" gorm:"type:text"`
	InheritanceMode     string             `json:"inheritanceMode,omitempty" gorm:"size:50"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (SNVIndel) TableName() string {
	return "result_snv_indels"
}

// SNVIndelListQuery query parameters
type SNVIndelListQuery struct {
	TaskID         string `form:"taskId"`
	Search         string `form:"search"`
	Gene           string `form:"gene"`
	Classification string `form:"classification"`
	GeneListID     string `form:"geneListId"`
	Page           int    `form:"page" binding:"min=1"`
	PageSize       int    `form:"page_size" binding:"min=1,max=100"`
}
