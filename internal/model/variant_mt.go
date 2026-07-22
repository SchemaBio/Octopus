package model

// MitochondrialVariant represents a mitochondrial variant (40 columns from mt_report.txt)
type MitochondrialVariant struct {
	ID                  string   `json:"id" gorm:"primaryKey;size:36"`
	TaskID              string   `json:"taskId" gorm:"size:36;index"`
	Chromosome          string   `json:"chromosome" gorm:"size:10"`
	Position            int64    `json:"position"`
	MTGene              string   `json:"mtGene" gorm:"size:100;index"`
	MTGeneType          string   `json:"mtGeneType" gorm:"size:50"` // rRNA, protein, tRNA, noncoding
	MitophenVariant     string   `json:"mitophenVariant,omitempty" gorm:"size:200"`
	MitophenPhenotypes  string   `json:"mitophenPhenotypes,omitempty" gorm:"type:text"`
	MTHGVS              string   `json:"mtHgvs" gorm:"size:200"`
	Ref                 string   `json:"ref" gorm:"size:1000"`
	Alt                 string   `json:"alt" gorm:"size:1000"`
	VariantType         string   `json:"variantType" gorm:"size:20"` // SNP, INS, DEL, MNP
	Filter              string   `json:"filter" gorm:"size:50"`
	Genotype            string   `json:"genotype" gorm:"size:20"`
	Heteroplasmy        float64  `json:"heteroplasmy" gorm:"type:numeric"`
	HeteroplasmyClass   string   `json:"heteroplasmyClass" gorm:"size:30"` // Homoplasmy, Heteroplasmy
	Depth               int      `json:"depth" gorm:"type:integer"`
	AD                  string   `json:"ad,omitempty" gorm:"size:100"`
	AF                  string   `json:"af,omitempty" gorm:"size:100"`
	Gene                string   `json:"gene" gorm:"size:100;index"`
	Feature             string   `json:"feature,omitempty" gorm:"size:100"`
	Consequence         string   `json:"consequence" gorm:"size:200"`
	Impact              string   `json:"impact,omitempty" gorm:"size:20"`
	HGVS_c              string   `json:"hgvsc,omitempty" gorm:"size:200"`
	HGVS_p              string   `json:"hgvsp,omitempty" gorm:"size:200"`
	AminoAcids          string   `json:"aminoAcids,omitempty" gorm:"size:50"`
	ProteinPosition     string   `json:"proteinPosition,omitempty" gorm:"size:50"`
	ClinvarSig          string   `json:"clinvarSig,omitempty" gorm:"size:500"`
	ClinvarDN           string   `json:"clinvarDn,omitempty" gorm:"size:500"`
	ClinvarStar         string   `json:"clinvarStar,omitempty" gorm:"size:50"`
	GnomadAF            *float64 `json:"gnomadAF,omitempty" gorm:"type:numeric"`
	GnomadEasAF         *float64 `json:"gnomadEasAF,omitempty" gorm:"type:numeric"`
	DbSNP               string   `json:"dbSnp,omitempty" gorm:"size:200"`
	MaxAF               *float64 `json:"maxAF,omitempty" gorm:"type:numeric"`
	TLOD                string   `json:"tlod,omitempty" gorm:"size:100"`
	POPAF               string   `json:"popaf,omitempty" gorm:"size:100"`
	GERMQ               string   `json:"germq,omitempty" gorm:"size:50"`
	STRANDQ             string   `json:"strandq,omitempty" gorm:"size:50"`
	CONTQ               string   `json:"contq,omitempty" gorm:"size:50"`
	SEQQ                string   `json:"seqq,omitempty" gorm:"size:50"`
	MBQ                 string   `json:"mbq,omitempty" gorm:"size:50"`
	MMQ                 string   `json:"mmq,omitempty" gorm:"size:50"`
	MFRL                string   `json:"mfrl,omitempty" gorm:"size:50"`
	VariantReviewStatus `json:"reviewStatus" gorm:"embedded"`
}

func (MitochondrialVariant) TableName() string {
	return "result_mt_variants"
}

// MTListQuery query parameters
type MTListQuery struct {
	TaskID   string `form:"taskId"`
	Search   string `form:"search"`
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}
