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
