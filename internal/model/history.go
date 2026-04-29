package model

// DetectionRecord represents a single detection event in history
type DetectionRecord struct {
	RecordID        string `json:"recordId"`
	TaskID          string `json:"taskId"`
	TaskName        string `json:"taskName"`
	Pipeline        string `json:"pipeline"`
	PipelineVersion string `json:"pipelineVersion"`
	SampleID        string `json:"sampleId"`
	InternalID      string `json:"internalId"`
	ReviewedAt      string `json:"reviewedAt"`
	ReviewedBy      string `json:"reviewedBy"`
}

// HistoryListQuery is the common query for history endpoints
type HistoryListQuery struct {
	Search     string `form:"searchQuery"`
	Page       int    `form:"page"`
	PageSize   int    `form:"pageSize"`
	SortColumn string `form:"sortColumn"`
	SortDir    string `form:"sortDirection"`
}

// GroupedSNVIndel represents a grouped SNV/Indel in history
type GroupedSNVIndel struct {
	GroupID            string              `json:"groupId"`
	Gene               string              `json:"gene"`
	HGVSc              string              `json:"hgvsc"`
	HGVSp              string              `json:"hgvsp"`
	Transcript         string              `json:"transcript"`
	ACMGClassification ACMGClassification  `json:"acmgClassification"`
	Consequence        string              `json:"consequence"`
	RsID               string              `json:"rsId,omitempty"`
	ClinvarID          string              `json:"clinvarId,omitempty"`
	GnomadAF           *float64            `json:"gnomadAF,omitempty"`
	DetectionCount     int                 `json:"detectionCount"`
	FirstDetectedAt    string              `json:"firstDetectedAt"`
	LastDetectedAt     string              `json:"lastDetectedAt"`
	Records            []DetectionRecord   `json:"records"`
}

// GroupedCNVSegment represents a grouped CNV segment in history
type GroupedCNVSegment struct {
	GroupID         string            `json:"groupId"`
	Chromosome      string            `json:"chromosome"`
	StartPosition   int64             `json:"startPosition"`
	EndPosition     int64             `json:"endPosition"`
	Length          int64             `json:"length"`
	Type            string            `json:"type"`
	CopyNumber      int               `json:"copyNumber"`
	Genes           []string          `json:"genes"`
	Confidence      float64           `json:"confidence"`
	DetectionCount  int               `json:"detectionCount"`
	FirstDetectedAt string            `json:"firstDetectedAt"`
	LastDetectedAt  string            `json:"lastDetectedAt"`
	Records         []DetectionRecord `json:"records"`
}

// GroupedCNVExon represents a grouped CNV exon in history
type GroupedCNVExon struct {
	GroupID         string            `json:"groupId"`
	Gene            string            `json:"gene"`
	Transcript      string            `json:"transcript"`
	Exon            string            `json:"exon"`
	Chromosome      string            `json:"chromosome"`
	StartPosition   int64             `json:"startPosition"`
	EndPosition     int64             `json:"endPosition"`
	Type            string            `json:"type"`
	CopyNumber      int               `json:"copyNumber"`
	Ratio           float64           `json:"ratio"`
	Confidence      float64           `json:"confidence"`
	DetectionCount  int               `json:"detectionCount"`
	FirstDetectedAt string            `json:"firstDetectedAt"`
	LastDetectedAt  string            `json:"lastDetectedAt"`
	Records         []DetectionRecord `json:"records"`
}

// GroupedSTR represents a grouped STR in history
type GroupedSTR struct {
	GroupID         string            `json:"groupId"`
	Gene            string            `json:"gene"`
	Transcript      string            `json:"transcript"`
	Locus           string            `json:"locus"`
	RepeatUnit      string            `json:"repeatUnit"`
	NormalRangeMin  int               `json:"normalRangeMin"`
	NormalRangeMax  int               `json:"normalRangeMax"`
	Status          STRStatus         `json:"status"`
	MinRepeatCount  int               `json:"minRepeatCount"`
	MaxRepeatCount  int               `json:"maxRepeatCount"`
	DetectionCount  int               `json:"detectionCount"`
	FirstDetectedAt string            `json:"firstDetectedAt"`
	LastDetectedAt  string            `json:"lastDetectedAt"`
	Records         []DetectionRecord `json:"records"`
}

// GroupedMEI represents a grouped MEI in history
type GroupedMEI struct {
	GroupID            string             `json:"groupId"`
	Chromosome         string             `json:"chromosome"`
	Position           int64              `json:"position"`
	Gene               string             `json:"gene"`
	MEIType            MEIType            `json:"meiType"`
	Strand             string             `json:"strand"`
	Length             int64              `json:"length"`
	Impact             string             `json:"impact,omitempty"`
	ACMGClassification ACMGClassification `json:"acmgClassification,omitempty"`
	DetectionCount     int                `json:"detectionCount"`
	FirstDetectedAt    string             `json:"firstDetectedAt"`
	LastDetectedAt     string             `json:"lastDetectedAt"`
	Records            []DetectionRecord  `json:"records"`
}

// GroupedMTVariant represents a grouped MT variant in history
type GroupedMTVariant struct {
	GroupID          string                   `json:"groupId"`
	Position         int64                    `json:"position"`
	Ref              string                   `json:"ref"`
	Alt              string                   `json:"alt"`
	Gene             string                   `json:"gene"`
	Pathogenicity    MitochondrialPathogenicity `json:"pathogenicity"`
	AssociatedDisease string                  `json:"associatedDisease"`
	Haplogroup       string                   `json:"haplogroup,omitempty"`
	MinHeteroplasmy  float64                  `json:"minHeteroplasmy"`
	MaxHeteroplasmy  float64                  `json:"maxHeteroplasmy"`
	DetectionCount   int                      `json:"detectionCount"`
	FirstDetectedAt  string                   `json:"firstDetectedAt"`
	LastDetectedAt   string                   `json:"lastDetectedAt"`
	Records          []DetectionRecord        `json:"records"`
}

// GroupedUPDRegion represents a grouped UPD region in history
type GroupedUPDRegion struct {
	GroupID         string            `json:"groupId"`
	Chromosome      string            `json:"chromosome"`
	StartPosition   int64             `json:"startPosition"`
	EndPosition     int64             `json:"endPosition"`
	Length          int64             `json:"length"`
	Type            UPDType           `json:"type"`
	Genes           []string          `json:"genes"`
	ParentOfOrigin  ParentOfOrigin    `json:"parentOfOrigin,omitempty"`
	DetectionCount  int               `json:"detectionCount"`
	FirstDetectedAt string            `json:"firstDetectedAt"`
	LastDetectedAt  string            `json:"lastDetectedAt"`
	Records         []DetectionRecord `json:"records"`
}

// DashboardStats represents dashboard statistics
type DashboardStats struct {
	TotalSamples    int `json:"totalSamples"`
	PendingTasks    int `json:"pendingTasks"`
	RunningTasks    int `json:"runningTasks"`
	CompletedTasks  int `json:"completedTasks"`
}
