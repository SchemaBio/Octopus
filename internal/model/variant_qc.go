package model

// QCResult represents quality control metrics for a sample
// Flattened from the nested qc_result structure in outputs.resolved.json
type QCResult struct {
	ID     string `json:"id" gorm:"primaryKey;size:36"`
	TaskID string `json:"taskId" gorm:"size:36;uniqueIndex"`

	// Sample
	SampleID string `json:"sampleId" gorm:"size:100"`

	// fastp - after filtering
	TotalReads      int64   `json:"totalReads"`
	TotalBases      int64   `json:"totalBases"`
	Q20Rate         float64 `json:"q20Rate" gorm:"type:numeric"`
	Q30Rate         float64 `json:"q30Rate" gorm:"type:numeric"`
	GcContent       float64 `json:"gcContent" gorm:"type:numeric"`
	Read1MeanLength int     `json:"read1MeanLength" gorm:"type:integer"`
	Read2MeanLength int     `json:"read2MeanLength" gorm:"type:integer"`

	// xamdst
	AverageDepth        float64 `json:"averageDepth" gorm:"type:numeric"`
	DedupDepth          float64 `json:"dedupDepth" gorm:"type:numeric"`
	CoverageGt0x        float64 `json:"coverageGt0x" gorm:"type:numeric"`
	CoverageGte30x      float64 `json:"coverageGte30x" gorm:"type:numeric"`
	CoverageGte100x     float64 `json:"coverageGte100x" gorm:"type:numeric"`
	MappedReads         int64   `json:"mappedReads"`
	MappedReadsFraction float64 `json:"mappedReadsFraction" gorm:"type:numeric"`
	InsertSizeAverage   float64 `json:"insertSizeAverage" gorm:"type:numeric"`
	InsertSizeMedian    int     `json:"insertSizeMedian" gorm:"type:integer"`
	RegionLength        int64   `json:"regionLength"`
	TargetDataFraction  float64 `json:"targetDataFraction" gorm:"type:numeric"`

	// hs_metrics
	MeanTargetCoverage   float64 `json:"meanTargetCoverage" gorm:"type:numeric"`
	MedianTargetCoverage int     `json:"medianTargetCoverage" gorm:"type:integer"`
	PctTargetBases30x    float64 `json:"pctTargetBases30x" gorm:"type:numeric"`
	PctTargetBases100x   float64 `json:"pctTargetBases100x" gorm:"type:numeric"`
	FoldEnrichment       float64 `json:"foldEnrichment" gorm:"type:numeric"`
	ZeroCvgTargetsPct    float64 `json:"zeroCvgTargetsPct" gorm:"type:numeric"`

	// sambamba
	DuplicateRate float64 `json:"duplicateRate" gorm:"type:numeric"`

	// sry
	PredictedGender string `json:"predictedGender" gorm:"size:20"`
	SryCount        int    `json:"sryCount" gorm:"type:integer"`

	// mt_xamdst
	MtAverageDepth float64 `json:"mtAverageDepth" gorm:"type:numeric"`
	MtCoverageGt0x float64 `json:"mtCoverageGt0x" gorm:"type:numeric"`

	// fingerprint
	FingerprintHash string `json:"fingerprintHash" gorm:"size:50"`

	// metrics
	PfMismatchRate float64 `json:"pfMismatchRate" gorm:"type:numeric"`
}

func (QCResult) TableName() string {
	return "result_qc"
}
