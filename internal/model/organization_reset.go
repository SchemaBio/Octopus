package model

import "time"

// A receipt survives business-data deletion so replay cannot erase a new workspace.
type OrganizationReset struct {
	OrgID     string `gorm:"index;size:36;not null" json:"org_id"`
	ID        string `gorm:"primaryKey;size:36" json:"id"`
	State     string `gorm:"size:20;not null" json:"state"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
