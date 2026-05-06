package model

// CNVSegment represents a CNV region-level segment result (42 columns from region.cnvanno.txt)
type CNVSegment struct {
	ID                    string             `json:"id" gorm:"primaryKey;size:36"`
	TaskID                string             `json:"taskId" gorm:"size:36;index"`
	Chromosome            string             `json:"chromosome" gorm:"size:10"`
	StartPosition         int64              `json:"startPosition"`
	EndPosition           int64              `json:"endPosition"`
	Type                  string             `json:"type" gorm:"size:20"` // DUP, DEL, Normal
	Log2Ratio             *float64           `json:"log2Ratio,omitempty" gorm:"type:numeric"`
	Depth                 *float64           `json:"depth,omitempty" gorm:"type:numeric"`
	Weight                *float64           `json:"weight,omitempty" gorm:"type:numeric"`
	CopyRatio             *float64           `json:"copyRatio,omitempty" gorm:"type:numeric"`
	ISCN                  string             `json:"iscn,omitempty" gorm:"size:200"`
	GeneCount             int                `json:"geneCount" gorm:"type:integer"`
	HIMax                 *float64           `json:"hiMax,omitempty" gorm:"type:numeric"`
	TRMax                 *float64           `json:"trMax,omitempty" gorm:"type:numeric"`
	MaxFrequency          *float64           `json:"maxFrequency,omitempty" gorm:"type:numeric"`
	Section1              *float64           `json:"section1,omitempty" gorm:"type:numeric"`
	Section2              *float64           `json:"section2,omitempty" gorm:"type:numeric"`
	Section3              *float64           `json:"section3,omitempty" gorm:"type:numeric"`
	Section4              *float64           `json:"section4,omitempty" gorm:"type:numeric"`
	Section5              *float64           `json:"section5,omitempty" gorm:"type:numeric"`
	TotalScore            *float64           `json:"totalScore,omitempty" gorm:"type:numeric"`
	Evidence1A            *float64           `json:"evidence1A,omitempty" gorm:"type:numeric"`
	Evidence1B            *float64           `json:"evidence1B,omitempty" gorm:"type:numeric"`
	Evidence2A            *float64           `json:"evidence2A,omitempty" gorm:"type:numeric"`
	Evidence2B            *float64           `json:"evidence2B,omitempty" gorm:"type:numeric"`
	Evidence2C            *float64           `json:"evidence2C,omitempty" gorm:"type:numeric"`
	Evidence2D            *float64           `json:"evidence2D,omitempty" gorm:"type:numeric"`
	Evidence2E            *float64           `json:"evidence2E,omitempty" gorm:"type:numeric"`
	Evidence2F            *float64           `json:"evidence2F,omitempty" gorm:"type:numeric"`
	Evidence2H            *float64           `json:"evidence2H,omitempty" gorm:"type:numeric"`
	Evidence2K            *float64           `json:"evidence2K,omitempty" gorm:"type:numeric"`
	Evidence3             *float64           `json:"evidence3,omitempty" gorm:"type:numeric"`
	Evidence4O            *float64           `json:"evidence4O,omitempty" gorm:"type:numeric"`
	Evidence4A            *float64           `json:"evidence4A,omitempty" gorm:"type:numeric"`
	Evidence4L            *float64           `json:"evidence4L,omitempty" gorm:"type:numeric"`
	Evidence5             *float64           `json:"evidence5,omitempty" gorm:"type:numeric"`
	DosageGenes           string             `json:"dosageGenes,omitempty" gorm:"type:text"`
	PathogenicRegions     string             `json:"pathogenicRegions,omitempty" gorm:"type:text"`
	BenignRegionsOverlap  string             `json:"benignRegionsOverlap,omitempty" gorm:"type:text"`
	GenCCADGenes          string             `json:"genccADGenes,omitempty" gorm:"type:text"`
	Classification        string             `json:"classification" gorm:"size:50"`
	Reason                string             `json:"reason,omitempty" gorm:"type:text"`
	EvidenceDetails       string             `json:"evidenceDetails,omitempty" gorm:"type:text"`
	VariantReviewStatus   `json:"reviewStatus" gorm:"embedded"`
}

func (CNVSegment) TableName() string {
	return "result_cnv_segments"
}

// CNVSegmentListQuery query parameters
type CNVSegmentListQuery struct {
	TaskID   string `form:"taskId" binding:"required"`
	Search   string `form:"search"`
	Type     string `form:"type"`
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
}
