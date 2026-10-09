package model

import "time"

// IGVSnapshot is browser-rendered evidence, scoped to one immutable execution.
type IGVSnapshot struct {
	ID                 string `gorm:"primaryKey;size:36"`
	Identity           string `gorm:"uniqueIndex;size:64;not null"`
	TaskUUID           string `gorm:"index;size:36;not null"`
	ExecutionAttemptID string `gorm:"size:36;not null"`
	TenantID           string `gorm:"size:36;not null"`
	Reference          string `gorm:"size:32;not null"`
	Locus              string `gorm:"size:100;not null"`
	EvidenceVersion    string `gorm:"size:64;not null"`
	ObjectKey          string `gorm:"not null"`
	CreatedAt          time.Time
}
