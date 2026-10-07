package model

import "time"

// A small immutable context, not per-row automatic results. Reassessment creates
// another version; old contexts remain available for interpretation provenance.
type ResultAssessmentContext struct {
	ID                 string `gorm:"primaryKey;size:64"`
	TenantID           string `gorm:"uniqueIndex:assessment_context_scope;size:160"`
	TaskUUID           string `gorm:"uniqueIndex:assessment_context_scope;size:36"`
	ExecutionAttemptID string `gorm:"uniqueIndex:assessment_context_scope;size:36"`
	Version            string `gorm:"uniqueIndex:assessment_context_scope;size:64"`
	PayloadJSON        string `gorm:"type:jsonb;not null"`
	CreatedAt          time.Time
	ActivatedAt        time.Time
}

// Reference content is shared by hash; contexts never duplicate a complete
// ontology/annotation bundle for every task or HPO revision.
type ResultAssessmentArtifact struct {
	ID          string `gorm:"primaryKey;size:64"`
	PayloadJSON string `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time
}
