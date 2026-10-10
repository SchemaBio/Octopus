package model

import "time"

// ResultDownload is a durable quote and billing idempotency key. Credentials
// and signed URLs are never persisted. A quote is scoped to one user/attempt.
type ResultDownload struct {
	ID             string     `gorm:"primaryKey;size:36" json:"id"`
	TaskUUID       string     `gorm:"size:36;index" json:"-"`
	AttemptID      string     `gorm:"size:36;index" json:"attempt_id"`
	OrgID          string     `gorm:"size:100" json:"-"`
	UserID         uint       `json:"-"`
	ClientIP       string     `gorm:"size:64" json:"-"`
	Kind           string     `gorm:"size:16" json:"kind"`
	ObjectKey      string     `gorm:"size:1000" json:"-"`
	Filename       string     `gorm:"size:255" json:"filename"`
	SizeBytes      int64      `json:"size_bytes"`
	ETag           string     `gorm:"size:128" json:"-"`
	Credits        int        `json:"credits"`
	QuoteExpiresAt time.Time  `json:"quote_expires_at"`
	LinkExpiresAt  *time.Time `json:"expires_at,omitempty"`
	ChargedAt      *time.Time `json:"charged_at,omitempty"`
	LastIssuedAt   *time.Time `json:"last_issued_at,omitempty"`
	IssueCount     int        `gorm:"not null;default:0" json:"issue_count"`
	RefundedAt     *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"-"`
	UpdatedAt      time.Time  `json:"-"`
}

type RawResultPackage struct {
	ID        string `gorm:"primaryKey;size:64"`
	ObjectKey string `gorm:"size:1000"`
	Status    string `gorm:"size:16"`
	Error     string `gorm:"size:255"`
	UpdatedAt time.Time
}
