package model

import "time"

// Personal view state contains query preferences, never result data or URLs.
type ResultSavedView struct {
	ID        string    `json:"id" gorm:"primaryKey;size:36"`
	TenantID  string    `json:"-" gorm:"size:160;uniqueIndex:idx_result_personal_view,priority:1"`
	TaskUUID  string    `json:"-" gorm:"size:36;uniqueIndex:idx_result_personal_view,priority:2"`
	UserID    uint      `json:"-" gorm:"uniqueIndex:idx_result_personal_view,priority:3"`
	Table     string    `json:"table" gorm:"size:64;uniqueIndex:idx_result_personal_view,priority:4"`
	Name      string    `json:"name" gorm:"size:80;uniqueIndex:idx_result_personal_view,priority:5"`
	StateJSON string    `json:"stateJson" gorm:"type:jsonb;not null"`
	Version   uint64    `json:"version" gorm:"not null;default:1"`
	UpdatedAt time.Time `json:"updatedAt"`
}
