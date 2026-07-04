package model

// CNVExon represents a CNV gene-level exon result (51 columns from gene.cnvanno.txt)
type CNVExon struct {
	ID                   string   `json:"id" gorm:"primaryKey;size:36"`
	TaskID               string   `json:"taskId" gorm:"size:36;index"`
	Chromosome           string   `json:"chromosome" gorm:"size:10"`
	StartPosition        int64    `json:"startPosition"`
	EndPosition          int64    `json:"endPosition"`
	Type                 string   `json:"type" gorm:"size:20"` // DUP, DEL, Normal
	Gene                 string   `json:"gene" gorm:"size:100;index"`
	Transcript           string   `json:"transcript" gorm:"size:100"`
	EnsemblTranscript    string   `json:"ensemblTranscript,omitempty" gorm:"size:100"`
	ExonCount            int      `json:"exonCount" gorm:"type:integer"`
	Log2Ratio            *float64 `json:"log2Ratio,omitempty" gorm:"type:numeric"`
	CopyRatio            *float64 `json:"copyRatio,omitempty" gorm:"type:numeric"`
	Weight               *float64 `json:"weight,omitempty" gorm:"type:numeric"`
	DepthRatio           *float64 `json:"depthRatio,omitempty" gorm:"type:numeric"`
	Depth                *float64 `json:"depth,omitempty" gorm:"type:numeric"`
	Quality              *float64 `json:"quality,omitempty" gorm:"type:numeric"`
	Ratio2               *float64 `json:"ratio2,omitempty" gorm:"type:numeric"`
	Flag1                int      `json:"flag1" gorm:"type:integer"`
	Flag2                int      `json:"flag2" gorm:"type:integer"`
	FloatFlag            *float64 `json:"floatFlag,omitempty" gorm:"type:numeric"`
	Impact               string   `json:"impact,omitempty" gorm:"size:20"` // LOW, MODERATE, HIGH
	ISCN                 string   `json:"iscn,omitempty" gorm:"size:200"`
	GeneCount            int      `json:"geneCount" gorm:"type:integer"`
	HIMax                *float64 `json:"hiMax,omitempty" gorm:"type:numeric"`
	TRMax                *float64 `json:"trMax,omitempty" gorm:"type:numeric"`
	MaxFrequency         *float64 `json:"maxFrequency,omitempty" gorm:"type:numeric"`
	Section1             *float64 `json:"section1,omitempty" gorm:"type:numeric"`
	Section2             *float64 `json:"section2,omitempty" gorm:"type:numeric"`
	Section3             *float64 `json:"section3,omitempty" gorm:"type:numeric"`
	Section4             *float64 `json:"section4,omitempty" gorm:"type:numeric"`
	Section5             *float64 `json:"section5,omitempty" gorm:"type:numeric"`
	TotalScore           *float64 `json:"totalScore,omitempty" gorm:"type:numeric"`
	Evidence1A           *float64 `json:"evidence1A,omitempty" gorm:"type:numeric"`
	Evidence1B           *float64 `json:"evidence1B,omitempty" gorm:"type:numeric"`
	Evidence2A           *float64 `json:"evidence2A,omitempty" gorm:"type:numeric"`
	Evidence2B           *float64 `json:"evidence2B,omitempty" gorm:"type:numeric"`
	Evidence2C           *float64 `json:"evidence2C,omitempty" gorm:"type:numeric"`
	Evidence2D           *float64 `json:"evidence2D,omitempty" gorm:"type:numeric"`
	Evidence2E           *float64 `json:"evidence2E,omitempty" gorm:"type:numeric"`
	Evidence2F           *float64 `json:"evidence2F,omitempty" gorm:"type:numeric"`
	Evidence2H           *float64 `json:"evidence2H,omitempty" gorm:"type:numeric"`
	Evidence2K           *float64 `json:"evidence2K,omitempty" gorm:"type:numeric"`
	Evidence3            *float64 `json:"evidence3,omitempty" gorm:"type:numeric"`
	Evidence4O           *float64 `json:"evidence4O,omitempty" gorm:"type:numeric"`
	Evidence4A           *float64 `json:"evidence4A,omitempty" gorm:"type:numeric"`
	Evidence4L           *float64 `json:"evidence4L,omitempty" gorm:"type:numeric"`
	Evidence5            *float64 `json:"evidence5,omitempty" gorm:"type:numeric"`
	DosageGenes          string   `json:"dosageGenes,omitempty" gorm:"type:text"`
	PathogenicRegions    string   `json:"pathogenicRegions,omitempty" gorm:"type:text"`
	BenignRegionsOverlap string   `json:"benignRegionsOverlap,omitempty" gorm:"type:text"`
	GenCCADGenes         string   `json:"genccADGenes,omitempty" gorm:"type:text"`
	Classification       string   `json:"classification" gorm:"size:50"`
	Reason               string   `json:"reason,omitempty" gorm:"type:text"`
	EvidenceDetails      string   `json:"evidenceDetails,omitempty" gorm:"type:text"`
	VariantReviewStatus  `json:"reviewStatus" gorm:"embedded"`
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
