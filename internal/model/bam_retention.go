package model

import "time"

// A completion snapshot survives retries, task deletion and result reimports.
type BAMRetentionJob struct {
	ID             string     `gorm:"primaryKey;size:36" json:"id"`
	TaskUUID       string     `gorm:"uniqueIndex:bam_attempt;size:36" json:"task_uuid"`
	OrgID          string     `gorm:"uniqueIndex:bam_attempt;size:36" json:"org_id"`
	AttemptID      string     `gorm:"uniqueIndex:bam_attempt;size:36" json:"attempt_id"`
	Bucket         string     `json:"bucket"`
	CompletedAt    time.Time  `json:"completed_at"`
	ExpiresAt      time.Time  `gorm:"index" json:"expires_at"`
	Status         string     `gorm:"index;size:32" json:"status"`
	ManifestKey    string     `json:"manifest_key"`
	ManifestSHA256 string     `json:"manifest_sha256"`
	PlanJSON       string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	LeaseID        string     `json:"-"`
	LeaseUntil     *time.Time `json:"-"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	Attempts       int        `json:"attempts"`
	ErrorCode      string     `json:"error_code,omitempty"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type BAMRetentionEvent struct {
	ID        uint64 `gorm:"primaryKey"`
	JobID     string `gorm:"index;size:36"`
	Action    string `gorm:"size:32"`
	ObjectKey string
	ErrorCode string `gorm:"size:80"`
	CreatedAt time.Time
}
