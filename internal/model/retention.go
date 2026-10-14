package model

import "time"

// Contains only cleanup identity and retry metadata; removed after destruction.
type RetentionJob struct {
	ID          string     `gorm:"primaryKey;size:80"`
	Kind        string     `gorm:"size:16;not null"`
	ResourceID  string     `gorm:"size:36;not null"`
	OrgID       string     `gorm:"size:100"`
	DueAt       time.Time  `gorm:"index;not null"`
	LeaseID     string     `gorm:"size:36"`
	LeaseUntil  *time.Time `gorm:"index"`
	NextRetryAt *time.Time `gorm:"index"`
	Attempts    int
	LastError   string `gorm:"size:64"`
}

func TaskRetentionDeadline(created time.Time) time.Time { return created.UTC().Add(720 * time.Hour) }
