package model

import "time"

// ResultDownload is a durable quote and billing idempotency key. Credentials
// and signed URLs are never persisted. A quote is scoped to one user/attempt.
type ResultDownload struct {
	ID                    string                  `gorm:"primaryKey;size:36" json:"id"`
	TaskUUID              string                  `gorm:"size:36;index" json:"-"`
	AttemptID             string                  `gorm:"size:36;index" json:"attempt_id"`
	OrgID                 string                  `gorm:"size:100" json:"-"`
	UserID                uint                    `json:"-"`
	ClientIP              string                  `gorm:"size:64" json:"-"`
	Kind                  string                  `gorm:"size:16" json:"kind"`
	ObjectKey             string                  `gorm:"size:1000" json:"-"`
	Filename              string                  `gorm:"size:255" json:"filename"`
	SizeBytes             int64                   `json:"size_bytes"`
	ETag                  string                  `gorm:"size:128" json:"-"`
	Credits               int                     `json:"credits"`
	QuoteExpiresAt        time.Time               `json:"quote_expires_at"`
	LinkExpiresAt         *time.Time              `json:"expires_at,omitempty"`
	ChargedAt             *time.Time              `json:"charged_at,omitempty"`
	LastIssuedAt          *time.Time              `json:"last_issued_at,omitempty"`
	IssueCount            int                     `gorm:"not null;default:0" json:"issue_count"`
	RefundedAt            *time.Time              `json:"-"`
	CreatedAt             time.Time               `json:"-"`
	UpdatedAt             time.Time               `json:"-"`
	ProtectionVersion     int                     `json:"protection_version" gorm:"not null;default:0"`
	BoundIP               string                  `json:"bound_ip,omitempty"`
	TrafficThresholdBytes int64                   `json:"traffic_threshold_bytes" gorm:"not null;default:0"`
	ObservedBytes         int64                   `json:"observed_bytes" gorm:"not null;default:0"`
	UsageSyncedAt         *time.Time              `json:"usage_synced_at,omitempty"`
	IssuanceState         string                  `json:"issuance_state" gorm:"not null;default:''"`
	Revision              uint64                  `json:"revision" gorm:"not null;default:0"`
	NetworkRequest        *DownloadNetworkRequest `json:"network_request,omitempty" gorm:"-"`
	FileID                string                  `json:"file_id,omitempty" gorm:"-"`
	ProtectionPlanned     bool                    `json:"protection_planned" gorm:"-"`
}

// These records deliberately contain no bearer URL, STS secret or token.
type DownloadIssuance struct {
	ID                  string    `gorm:"primaryKey;size:36" json:"id"`
	GrantID             string    `gorm:"index;size:36" json:"grant_id"`
	KeyHash             string    `gorm:"uniqueIndex;size:64" json:"-"`
	ObjectHash          string    `json:"-"`
	Bucket              string    `json:"-"`
	BoundIP             string    `json:"bound_ip"`
	ExpiresAt           time.Time `json:"expires_at"`
	CredentialExpiresAt time.Time `json:"credential_expires_at"`
	CreatedAt           time.Time `json:"created_at"`
}

type DownloadUsageEvent struct {
	ID        string `gorm:"primaryKey;size:64"`
	GrantID   string `gorm:"index;size:36"`
	Bytes     int64
	EventTime time.Time
	CreatedAt time.Time
}

type DownloadUsageSync struct {
	ID            string     `gorm:"primaryKey;size:32" json:"-"`
	Cursor        time.Time  `json:"cursor"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastReplayAt  *time.Time `json:"last_replay_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

type DownloadNetworkRequest struct {
	ID        string     `gorm:"primaryKey;size:36" json:"id"`
	GrantID   string     `gorm:"index;size:36" json:"grant_id"`
	TargetIP  string     `json:"target_ip"`
	Status    string     `json:"status"`
	NotBefore *time.Time `json:"not_before,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Revision  uint64     `json:"revision"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type DownloadProtectionEvent struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`
	GrantID   string    `gorm:"index;size:36" json:"grant_id"`
	Action    string    `json:"action"`
	Actor     uint      `json:"actor"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type RawResultPackage struct {
	ID        string `gorm:"primaryKey;size:64"`
	TaskUUID  string `gorm:"size:36;index"`
	ObjectKey string `gorm:"size:1000"`
	Status    string `gorm:"size:16"`
	Error     string `gorm:"size:255"`
	UpdatedAt time.Time
}
