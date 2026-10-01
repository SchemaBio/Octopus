package model

import (
	"encoding/json"
	"time"
)

// ResultDataset describes one immutable Parquet object for one task attempt.
type ResultDataset struct {
	AdjustmentRevision         uint64    `json:"adjustmentRevision" gorm:"not null;default:0"`
	ID                         string    `json:"id" gorm:"primaryKey;size:64"`
	TenantID                   string    `json:"-" gorm:"size:160;index;uniqueIndex:idx_result_dataset_scope,priority:1"`
	TaskUUID                   string    `json:"taskUuid" gorm:"size:36;index;uniqueIndex:idx_result_dataset_scope,priority:2"`
	ExecutionAttemptID         string    `json:"executionAttemptId" gorm:"size:36;index;uniqueIndex:idx_result_dataset_scope,priority:3"`
	Table                      string    `json:"table" gorm:"size:64;uniqueIndex:idx_result_dataset_scope,priority:4"`
	ObjectKey                  string    `json:"-" gorm:"type:text;not null"`
	ObjectSHA256               string    `json:"objectSha256" gorm:"size:64;not null"`
	ManifestVersion            string    `json:"manifestVersion" gorm:"size:64;not null;default:''"`
	SourceSize                 int64     `json:"-" gorm:"not null;default:0"`
	SourceLastModified         time.Time `json:"-" gorm:"type:timestamptz"`
	FieldsJSON                 string    `json:"-" gorm:"type:jsonb;not null"`
	AutomaticAssessmentProfile string    `json:"automaticAssessmentProfile" gorm:"size:64;not null;default:''"`
	AutomaticAssessmentReady   bool      `json:"automaticAssessmentReady" gorm:"not null;default:false"`
	ExpectedRows               *int64    `json:"expectedRows,omitempty"`
	Rows                       int64     `json:"rows"`
	DataVersion                string    `json:"dataVersion" gorm:"size:64;not null"`
	CreatedAt                  time.Time `json:"createdAt" gorm:"type:timestamptz"`
}

func (ResultDataset) TableName() string { return "result_datasets" }

type ResultRowAutomaticAssessment struct {
	TenantID           string    `json:"-" gorm:"size:160;primaryKey"`
	TaskUUID           string    `json:"taskUuid" gorm:"size:36;primaryKey;index"`
	ExecutionAttemptID string    `json:"executionAttemptId" gorm:"size:36;primaryKey;index"`
	Table              string    `json:"table" gorm:"size:64;primaryKey"`
	RowID              string    `json:"rowId" gorm:"size:64;primaryKey"`
	ProfileVersion     string    `json:"profileVersion" gorm:"size:64;primaryKey"`
	AssessmentJSON     string    `json:"assessment" gorm:"type:jsonb;not null"`
	CreatedAt          time.Time `json:"createdAt" gorm:"type:timestamptz"`
}

func (ResultRowAutomaticAssessment) TableName() string { return "result_row_automatic_assessments" }

// ResultRowAdjustment is the current user-authored overlay for one stable row.
type ResultRowAdjustment struct {
	Revision           uint64    `json:"revision" gorm:"not null;default:0"`
	DatasetVersion     string    `json:"datasetVersion" gorm:"size:64;not null;default:''"`
	TenantID           string    `json:"-" gorm:"size:160;primaryKey"`
	TaskUUID           string    `json:"taskUuid" gorm:"size:36;primaryKey;index"`
	ExecutionAttemptID string    `json:"executionAttemptId" gorm:"size:36;primaryKey;index"`
	Table              string    `json:"table" gorm:"size:64;primaryKey"`
	RowID              string    `json:"rowId" gorm:"size:64;primaryKey"`
	PayloadJSON        string    `json:"adjustments" gorm:"type:jsonb;not null"`
	Version            uint64    `json:"version" gorm:"not null;default:1"`
	UpdatedBy          string    `json:"updatedBy" gorm:"size:160"`
	UpdatedAt          time.Time `json:"updatedAt" gorm:"type:timestamptz"`
}

func (ResultRowAdjustment) TableName() string { return "result_row_adjustments" }

type ResultRowAdjustmentEvent struct {
	ClientMutationID   *string   `json:"clientMutationId,omitempty" gorm:"size:36;uniqueIndex:idx_result_mutation,priority:2"`
	RequestSHA256      string    `json:"-" gorm:"size:64"`
	Revision           uint64    `json:"revision" gorm:"not null;default:0"`
	DatasetVersion     string    `json:"datasetVersion" gorm:"size:64;not null;default:''"`
	ID                 string    `json:"id" gorm:"primaryKey;size:36"`
	TenantID           string    `json:"-" gorm:"size:160;index;uniqueIndex:idx_result_mutation,priority:1"`
	TaskUUID           string    `json:"taskUuid" gorm:"size:36;index"`
	ExecutionAttemptID string    `json:"executionAttemptId" gorm:"size:36;index"`
	Table              string    `json:"table" gorm:"size:64"`
	RowID              string    `json:"rowId" gorm:"size:64;index"`
	BeforeJSON         string    `json:"before" gorm:"type:jsonb"`
	AfterJSON          string    `json:"after" gorm:"type:jsonb;not null"`
	Reason             string    `json:"reason" gorm:"type:text"`
	Actor              string    `json:"actor" gorm:"size:160"`
	CreatedAt          time.Time `json:"createdAt" gorm:"type:timestamptz;index"`
}

func (ResultRowAdjustmentEvent) TableName() string { return "result_row_adjustment_events" }

type ResultRowAdjustmentRequest struct {
	RowOrdinal       *int64                 `json:"rowOrdinal,omitempty"`
	ClientMutationID string                 `json:"clientMutationId"`
	DatasetVersion   string                 `json:"datasetVersion"`
	AttemptID        string                 `json:"attemptId"`
	ExpectedVersion  uint64                 `json:"expectedVersion"`
	Adjustments      map[string]interface{} `json:"adjustments" binding:"required"`
	Reason           string                 `json:"reason"`
}

// Keep JSONB storage private while returning a JSON object to API clients.
func (r ResultRowAdjustment) MarshalJSON() ([]byte, error) {
	type alias ResultRowAdjustment
	return json.Marshal(struct {
		alias
		Adjustments json.RawMessage `json:"adjustments"`
	}{alias(r), json.RawMessage(r.PayloadJSON)})
}
func (r ResultRowAdjustmentEvent) MarshalJSON() ([]byte, error) {
	type alias ResultRowAdjustmentEvent
	return json.Marshal(struct {
		alias
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
	}{alias(r), json.RawMessage(r.BeforeJSON), json.RawMessage(r.AfterJSON)})
}

// A failed identity proof remains pending and never silently transfers a review.
type ResultLegacyRowMapping struct {
	TenantID           string `json:"-" gorm:"primaryKey;size:160"`
	TaskUUID           string `json:"taskUuid" gorm:"primaryKey;size:36"`
	ExecutionAttemptID string `json:"attemptId" gorm:"primaryKey;size:36"`
	Table              string `json:"table" gorm:"primaryKey;size:64"`
	LegacyID           string `json:"legacyId" gorm:"primaryKey;size:36"`
	RowID              string `json:"rowId" gorm:"size:64"`
	DatasetVersion     string `json:"datasetVersion" gorm:"size:64"`
	Status             string `json:"status" gorm:"size:32"`
	Reason             string `json:"reason" gorm:"type:text"`
	UpdatedAt          time.Time
}
