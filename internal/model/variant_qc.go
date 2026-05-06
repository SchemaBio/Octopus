package model

// QCResult represents quality control metrics for a sample
type QCResult struct {
	ID                 string  `json:"id" gorm:"primaryKey;size:36"`
	TaskID             string  `json:"taskId" gorm:"size:36;uniqueIndex"`
	TotalReads         int64   `json:"totalReads"`
	MappedReads        int64   `json:"mappedReads"`
	MappingRate        float64 `json:"mappingRate" gorm:"type:numeric"`
	AverageDepth       float64 `json:"averageDepth" gorm:"type:numeric"`
	DedupDepth         float64 `json:"dedupDepth" gorm:"type:numeric"`
	TargetCoverage     float64 `json:"targetCoverage" gorm:"type:numeric"`
	DuplicateRate      float64 `json:"duplicateRate" gorm:"type:numeric"`
	Q30Rate            float64 `json:"q30Rate" gorm:"type:numeric"`
	InsertSize         float64 `json:"insertSize" gorm:"type:numeric"`
	GcRatio            float64 `json:"gcRatio" gorm:"type:numeric"`
	Uniformity         float64 `json:"uniformity" gorm:"type:numeric"`
	CaptureEfficiency  float64 `json:"captureEfficiency" gorm:"type:numeric"`
	PredictedGender    string  `json:"predictedGender" gorm:"size:20"` // Male, Female, Unknown
	ContaminationRate  float64 `json:"contaminationRate" gorm:"type:numeric"`
	MtCoverage         float64 `json:"mtCoverage" gorm:"type:numeric"`
	MtDepth            float64 `json:"mtDepth" gorm:"type:numeric"`
}

func (QCResult) TableName() string {
	return "result_qc"
}
