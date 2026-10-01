package model

// ResultContextResponse is the immutable execution context consumed by the
// interpretation workspace. It intentionally carries IDs and availability
// state, never archive paths or signed object URLs.
type ResultContextResponse struct {
	TaskUUID           string                 `json:"taskUuid"`
	ExecutionAttemptID string                 `json:"executionAttemptId"`
	ImportBatchID      uint                   `json:"importBatchId,omitempty"`
	ImportStatus       ResultImportStatus     `json:"importStatus"`
	State              string                 `json:"state"`
	Version            string                 `json:"version"`
	Reference          ResultReference        `json:"reference"`
	Members            []ResultMember         `json:"members"`
	Types              map[string]ResultCount `json:"types"`
	QC                 []QCMemberSummary      `json:"qc"`
	Permissions        ResultPermissions      `json:"permissions"`
	Parquet            ParquetResultState     `json:"parquet"`
}

type ParquetResultState struct {
	Available                  bool     `json:"available"`
	Tables                     []string `json:"tables"`
	PreparedTables             []string `json:"preparedTables"`
	ManifestVersion            string   `json:"manifestVersion,omitempty"`
	FieldProfileVersion        string   `json:"fieldProfileVersion,omitempty"`
	AutomaticAssessmentProfile string   `json:"automaticAssessmentProfile,omitempty"`
	Reason                     string   `json:"reason,omitempty"`
}

type ResultReference struct {
	// DeclaredID is read from the input snapshot. Available becomes true only
	// when an explicitly configured FASTA + FAI pair matches that declaration.
	DeclaredID string `json:"declaredId,omitempty"`
	Available  bool   `json:"available"`
	Reason     string `json:"reason,omitempty"`
}

type ResultMember struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	SampleID string `json:"sampleId,omitempty"`
}

type ResultCount struct {
	Total    int64 `json:"total"`
	Reviewed int64 `json:"reviewed"`
	Reported int64 `json:"reported"`
}

type ResultPermissions struct {
	CanReview bool `json:"canReview"`
	CanReport bool `json:"canReport"`
}

type QCMemberSummary struct {
	MemberID   string     `json:"memberId"`
	MemberRole string     `json:"memberRole"`
	SampleID   string     `json:"sampleId,omitempty"`
	Metrics    []QCMetric `json:"metrics"`
}

// QCMetric represents source-backed data. Value is nil when the source did
// not provide a metric, which is distinct from a valid measured value of zero.
type QCMetric struct {
	Key       string   `json:"key"`
	Value     *float64 `json:"value"`
	Unit      string   `json:"unit,omitempty"`
	Source    string   `json:"source,omitempty"`
	Threshold string   `json:"threshold,omitempty"`
}

// IGVSessionResponse describes eligible evidence files without exposing their
// storage location. The browser asks for URLs only for selected track IDs.
type IGVSessionResponse struct {
	TaskUUID           string               `json:"taskUuid"`
	ExecutionAttemptID string               `json:"executionAttemptId"`
	Version            string               `json:"version"`
	Available          bool                 `json:"available"`
	Reason             string               `json:"reason,omitempty"`
	Reference          IGVReferenceResponse `json:"reference"`
	Tracks             []IGVTrackDescriptor `json:"tracks"`
}

type IGVReferenceResponse struct {
	ID                string `json:"id,omitempty"`
	Available         bool   `json:"available"`
	Reason            string `json:"reason,omitempty"`
	FASTAURL          string `json:"fastaURL,omitempty"`
	IndexURL          string `json:"indexURL,omitempty"`
	AliasURL          string `json:"aliasURL,omitempty"`
	CytobandURL       string `json:"cytobandURL,omitempty"`
	GeneTrackURL      string `json:"geneTrackURL,omitempty"`
	GeneTrackIndexURL string `json:"geneTrackIndexURL,omitempty"`
}

type IGVTrackDescriptor struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Format     string `json:"format"`
	MemberID   string `json:"memberId,omitempty"`
	MemberRole string `json:"memberRole,omitempty"`
	HasIndex   bool   `json:"hasIndex"`
	Available  bool   `json:"available"`
	Reason     string `json:"reason,omitempty"`
}

type IGVURLRequest struct {
	TrackIDs []string `json:"trackIds" binding:"required,min=1,max=20"`
	Version  string   `json:"version" binding:"required,max=256"`
}

type IGVTrackURL struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	IndexURL string `json:"indexURL,omitempty"`
}

type IGVURLResponse struct {
	Tracks    []IGVTrackURL `json:"tracks"`
	ExpiresAt string        `json:"expiresAt"`
}
