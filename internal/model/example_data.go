package model

import "time"

// These tables are migrated only by the SaaS startup path.
type ExampleTemplate struct {
	Version        string `gorm:"primaryKey;size:64"`
	ManifestJSON   string `gorm:"type:jsonb;not null"`
	ManifestSHA256 string `gorm:"size:64;not null"`
	CreatedAt      time.Time
}
type ExampleTemplateActive struct {
	ID      uint   `gorm:"primaryKey"`
	Version string `gorm:"size:64;not null"`
}
type ExampleDataGrant struct {
	ID              string     `gorm:"primaryKey;size:36" json:"id"`
	OrgID           string     `gorm:"uniqueIndex;size:100;not null" json:"org_id"`
	UserID          uint       `json:"user_id"`
	TemplateVersion string     `gorm:"size:64;not null" json:"template_version"`
	ApprovedAt      time.Time  `json:"approved_at"`
	Status          string     `gorm:"size:32;index" json:"status"`
	SampleUUID      string     `gorm:"size:36;not null" json:"sample_id"`
	TaskUUID        string     `gorm:"size:36;not null" json:"task_id"`
	AttemptUUID     string     `gorm:"size:36;not null" json:"-"`
	Read1UUID       string     `gorm:"size:36;not null" json:"-"`
	Read2UUID       string     `gorm:"size:36;not null" json:"-"`
	NextRetryAt     time.Time  `gorm:"index" json:"-"`
	LeaseID         string     `gorm:"size:36" json:"-"`
	LeaseUntil      *time.Time `gorm:"index" json:"-"`
	LastError       string     `gorm:"size:64" json:"error_code,omitempty"`
	CreatedAt       time.Time  `json:"-"`
	UpdatedAt       time.Time  `json:"-"`
}
type ExampleDataObject struct {
	GrantID    string `gorm:"primaryKey;size:36"`
	Path       string `gorm:"primaryKey;size:1000"`
	ObjectKey  string `gorm:"size:1200;not null"`
	SHA256     string `gorm:"size:64;not null"`
	VerifiedAt *time.Time
}
