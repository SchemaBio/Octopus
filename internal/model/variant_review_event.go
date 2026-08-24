package model

import "time"

// VariantReviewAction is an append-only state transition in the review audit
// stream. The current result row is only a fast read projection.
type VariantReviewAction string

const (
	VariantReviewActionReviewed VariantReviewAction = "REVIEWED"
	VariantReviewActionRevoked  VariantReviewAction = "REVOKED"
)

// VariantReviewEvent records a review transition independently of the result
// row, which may be replaced during a structured-result re-import.
type VariantReviewEvent struct {
	ID                  string              `json:"id" gorm:"primaryKey;size:36"`
	TenantID            string              `json:"-" gorm:"size:160;index;not null"`
	TaskUUID            string              `json:"taskUuid" gorm:"size:36;index;not null"`
	ExecutionAttemptID  string              `json:"executionAttemptId" gorm:"size:36;index;not null"`
	ImportBatchID       uint                `json:"importBatchId" gorm:"index"`
	VariantType         string              `json:"variantType" gorm:"size:30;index;not null"`
	VariantID           string              `json:"variantId" gorm:"size:100;index;not null"`
	VariantFingerprint  string              `json:"variantFingerprint" gorm:"size:64;index;not null"`
	HistoryGroupKey     string              `json:"historyGroupKey" gorm:"size:500;index;not null"`
	Action              VariantReviewAction `json:"action" gorm:"size:20;index;not null"`
	ActorUserID         uint                `json:"actorUserId" gorm:"index"`
	ActorEmail          string              `json:"actorEmail" gorm:"size:100"`
	ReferenceGenome     string              `json:"referenceGenome" gorm:"size:20"`
	TaskName            string              `json:"taskName" gorm:"size:200"`
	Pipeline            string              `json:"pipeline" gorm:"size:200"`
	PipelineVersion     string              `json:"pipelineVersion" gorm:"size:50"`
	SampleID            string              `json:"sampleId" gorm:"size:100"`
	InternalID          string              `json:"internalId" gorm:"size:100"`
	OccurredAt          *time.Time          `json:"occurredAt,omitempty" gorm:"type:timestamptz"`
	TimestampKnown      bool                `json:"timestampKnown" gorm:"not null;default:true"`
	RecordedAt          time.Time           `json:"recordedAt" gorm:"type:timestamptz;not null;autoCreateTime;index"`
	VariantSnapshotJSON string              `json:"-" gorm:"type:jsonb;not null"`
}

func (VariantReviewEvent) TableName() string { return "variant_review_events" }

// VariantReviewEventResponse is safe for frontend audit display.
type VariantReviewEventResponse struct {
	ID                 string              `json:"id"`
	TaskUUID           string              `json:"taskUuid"`
	ExecutionAttemptID string              `json:"executionAttemptId"`
	ImportBatchID      uint                `json:"importBatchId,omitempty"`
	VariantType        string              `json:"variantType"`
	VariantID          string              `json:"variantId"`
	VariantFingerprint string              `json:"variantFingerprint"`
	HistoryGroupKey    string              `json:"historyGroupKey"`
	Action             VariantReviewAction `json:"action"`
	ActorUserID        uint                `json:"actorUserId,omitempty"`
	ActorEmail         string              `json:"actorEmail,omitempty"`
	ReferenceGenome    string              `json:"referenceGenome,omitempty"`
	OccurredAt         *time.Time          `json:"occurredAt,omitempty"`
	TimestampKnown     bool                `json:"timestampKnown"`
	RecordedAt         time.Time           `json:"recordedAt"`
}

func (e VariantReviewEvent) ToResponse() VariantReviewEventResponse {
	return VariantReviewEventResponse{
		ID: e.ID, TaskUUID: e.TaskUUID, ExecutionAttemptID: e.ExecutionAttemptID,
		ImportBatchID: e.ImportBatchID, VariantType: e.VariantType, VariantID: e.VariantID,
		VariantFingerprint: e.VariantFingerprint, HistoryGroupKey: e.HistoryGroupKey, Action: e.Action,
		ActorUserID: e.ActorUserID, ActorEmail: e.ActorEmail,
		ReferenceGenome: e.ReferenceGenome, OccurredAt: e.OccurredAt,
		TimestampKnown: e.TimestampKnown, RecordedAt: e.RecordedAt,
	}
}

// VariantReviewEventListQuery is internal scope plus optional audit filters.
type VariantReviewEventListQuery struct {
	TaskUUID           string
	VariantType        string
	VariantFingerprint string
	ExecutionAttemptID string
	TenantID           string
	IncludeAll         bool
	Page               int
	PageSize           int
}
