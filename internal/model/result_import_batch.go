package model

import "time"

// ResultImportBatchStatus tracks one structured import attempt.
type ResultImportBatchStatus string

const (
	ResultImportBatchStatusRunning ResultImportBatchStatus = "running"
	ResultImportBatchStatusSuccess ResultImportBatchStatus = "success"
	ResultImportBatchStatusFailed  ResultImportBatchStatus = "failed"
)

// ResultImportBatch records file-level audit metadata for a local structured
// result import attempt in the open-source Octopus backend.
type ResultImportBatch struct {
	ID             uint                    `json:"id" gorm:"primaryKey"`
	TaskUUID       string                  `json:"task_uuid" gorm:"size:36;index;not null"`
	Source         string                  `json:"source" gorm:"size:20;not null"`
	Status         ResultImportBatchStatus `json:"status" gorm:"size:30;index;not null"`
	Fingerprint    string                  `json:"fingerprint" gorm:"size:64;index;not null"`
	ArchiveBase    string                  `json:"archive_base" gorm:"type:text"`
	ArchivePrefix  string                  `json:"archive_prefix" gorm:"type:text"`
	OutputsKey     string                  `json:"outputs_key" gorm:"type:text"`
	ObjectKeysJSON string                  `json:"object_keys_json" gorm:"type:jsonb"`
	CountsJSON     string                  `json:"counts_json" gorm:"type:jsonb"`
	Error          string                  `json:"error" gorm:"type:text"`
	StartedAt      time.Time               `json:"started_at" gorm:"type:timestamptz;index"`
	FinishedAt     *time.Time              `json:"finished_at,omitempty" gorm:"type:timestamptz"`
	CreatedAt      time.Time               `json:"created_at" gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt      time.Time               `json:"updated_at" gorm:"autoUpdateTime;type:timestamptz"`
}

// TableName specifies the table name for ResultImportBatch.
func (ResultImportBatch) TableName() string {
	return "result_import_batches"
}

// ResultImportBatchListQuery is the query parameters for listing import
// batches (GET /result-import/batches). Org scoping is applied by joining
// to tasks (ResultImportBatch carries no org column itself).
type ResultImportBatchListQuery struct {
	Page     int                     `form:"page" binding:"min=1"`
	PageSize int                     `form:"page_size" binding:"min=1,max=100"`
	Status   ResultImportBatchStatus `form:"status"` // filter; "failed" for risk signals
	Since    *time.Time               `form:"since"` // started_at >= since (RFC3339); nil = no filter
	// internal scope (set by handler, not bound from query):
	IncludeAll    bool   `json:"-"`
	ExternalOrgID string `json:"-"`
	UserID        uint   `json:"-"`
}

// ResultImportBatchResponse is the audit shape for an import batch.
type ResultImportBatchResponse struct {
	ID          uint                    `json:"id"`
	TaskUUID    string                  `json:"task_uuid"`
	Source      string                  `json:"source"`
	Status      ResultImportBatchStatus `json:"status"`
	Fingerprint string                  `json:"fingerprint"`
	Error       string                  `json:"error,omitempty"`
	StartedAt   string                  `json:"started_at"`
	FinishedAt  string                  `json:"finished_at,omitempty"`
	OrgID       string                  `json:"org_id,omitempty"` // from joined task
}

// ResultImportBatchListResponse is the list envelope for import batches.
type ResultImportBatchListResponse struct {
	Total int64                       `json:"total"`
	Items []ResultImportBatchResponse `json:"items"`
}
