package model

import "time"

type SampleDataLink struct {
	ID            uint            `json:"-" gorm:"primaryKey"`
	SampleID      uint            `json:"-" gorm:"uniqueIndex;not null"`
	ExternalOrgID string          `json:"-" gorm:"size:100;index"`
	Read1AssetID  uint            `json:"-" gorm:"index;not null"`
	Read2AssetID  uint            `json:"-" gorm:"index;not null"`
	MatchMode     SampleMatchMode `json:"match_mode" gorm:"size:20;index;not null"`
	MatchRule     string          `json:"match_rule,omitempty" gorm:"size:100"`
	MatchedBy     uint            `json:"matched_by" gorm:"index"`
	MatchedAt     time.Time       `json:"matched_at" gorm:"type:timestamptz"`
	CreatedAt     time.Time       `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt     time.Time       `json:"updated_at" gorm:"type:timestamptz"`
}

func (SampleDataLink) TableName() string { return "sample_data_links" }
