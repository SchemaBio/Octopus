package model

// QCResult represents quality control metrics for a sample
type QCResult struct {
	ID                 string  `json:"id" gorm:"primaryKey;size:36"`
	TaskID             string  `json:"taskId" gorm:"size:36;uniqueIndex"`
	TotalReads         int64   `json:"totalReads"`
	MappedReads        int64   `json:"mappedReads"`
	MappingRate        float64 `json:"mappingRate"`
	AverageDepth       float64 `json:"averageDepth"`
	DedupDepth         float64 `json:"dedupDepth"`
	TargetCoverage     float64 `json:"targetCoverage"`
	DuplicateRate      float64 `json:"duplicateRate"`
	Q30Rate            float64 `json:"q30Rate"`
	InsertSize         float64 `json:"insertSize"`
	GcRatio            float64 `json:"gcRatio"`
	Uniformity         float64 `json:"uniformity"`
	CaptureEfficiency  float64 `json:"captureEfficiency"`
	PredictedGender    string  `json:"predictedGender" gorm:"size:20"` // Male, Female, Unknown
	ContaminationRate  float64 `json:"contaminationRate"`
	MtCoverage         float64 `json:"mtCoverage"`
	MtDepth            float64 `json:"mtDepth"`
}

func (QCResult) TableName() string {
	return "result_qc"
}
