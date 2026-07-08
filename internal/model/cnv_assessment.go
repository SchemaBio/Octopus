package model

import (
	"encoding/json"
	"time"
)

// CNVAssessment stores a user-edited ClinGen CNV assessment payload for a
// CNV segment/exon result row. The payload is intentionally stored as JSON so
// the scoring UI can evolve without requiring a schema migration for every
// ClinGen rule-field change.
type CNVAssessment struct {
	ID          string    `json:"id" gorm:"primaryKey;size:36"`
	TaskID      string    `json:"task_id" gorm:"size:36;not null;uniqueIndex:idx_cnv_assessment_variant;index"`
	VariantType string    `json:"variant_type" gorm:"size:20;not null;uniqueIndex:idx_cnv_assessment_variant"`
	VariantID   string    `json:"variant_id" gorm:"size:100;not null;uniqueIndex:idx_cnv_assessment_variant"`
	PayloadJSON string    `json:"-" gorm:"type:jsonb;not null"`
	CreatedBy   string    `json:"created_by" gorm:"size:100"`
	UpdatedBy   string    `json:"updated_by" gorm:"size:100"`
	CreatedAt   time.Time `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"type:timestamptz"`
}

func (CNVAssessment) TableName() string {
	return "cnv_assessments"
}

// CNVAssessmentUpsertRequest is the request body for saving an assessment.
type CNVAssessmentUpsertRequest struct {
	Assessment json.RawMessage `json:"assessment" binding:"required"`
}

// CNVAssessmentListQuery filters assessment list results.
type CNVAssessmentListQuery struct {
	VariantType string `form:"type"`
	VariantIDs  string `form:"variant_ids"`
}

// CNVAssessmentResponse is returned to the frontend.
type CNVAssessmentResponse struct {
	ID          string          `json:"id"`
	TaskID      string          `json:"task_id"`
	VariantType string          `json:"variant_type"`
	VariantID   string          `json:"variant_id"`
	Assessment  json.RawMessage `json:"assessment"`
	CreatedBy   string          `json:"created_by,omitempty"`
	UpdatedBy   string          `json:"updated_by,omitempty"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

func CNVAssessmentToResponse(a *CNVAssessment) CNVAssessmentResponse {
	return CNVAssessmentResponse{
		ID:          a.ID,
		TaskID:      a.TaskID,
		VariantType: a.VariantType,
		VariantID:   a.VariantID,
		Assessment:  json.RawMessage(a.PayloadJSON),
		CreatedBy:   a.CreatedBy,
		UpdatedBy:   a.UpdatedBy,
		CreatedAt:   a.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   a.UpdatedAt.Format(time.RFC3339),
	}
}
