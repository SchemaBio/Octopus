package model

import "time"

// WorkflowThresholdConfig stores SaaS administrator overrides for the small
// set of tunable germline calling thresholds. Workflow resources remain in
// the code-owned catalog.
type WorkflowThresholdConfig struct {
	ID              uint      `json:"-" gorm:"primaryKey"`
	Template        string    `json:"template" gorm:"size:20;uniqueIndex:idx_workflow_threshold_scope;not null"`
	ReferenceGenome string    `json:"reference_genome" gorm:"size:20;uniqueIndex:idx_workflow_threshold_scope;not null"`
	SRYSexCutoff    float64   `json:"sry_sex_cutoff" gorm:"not null"`
	CNVBinSize      int       `json:"cnv_bin_size" gorm:"not null"`
	CNVDupThreshold float64   `json:"cnv_dup_threshold" gorm:"not null"`
	CNVDelThreshold float64   `json:"cnv_del_threshold" gorm:"not null"`
	UpdatedBy       uint      `json:"updated_by"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"autoUpdateTime;type:timestamptz"`
}

type WorkflowThresholdUpdateRequest struct {
	SRYSexCutoff    float64 `json:"sry_sex_cutoff" binding:"gte=0"`
	CNVBinSize      int     `json:"cnv_bin_size" binding:"required,gt=0"`
	CNVDupThreshold float64 `json:"cnv_dup_threshold" binding:"required,gt=0"`
	CNVDelThreshold float64 `json:"cnv_del_threshold" binding:"required,gt=0"`
}

func (WorkflowThresholdConfig) TableName() string { return "workflow_threshold_configs" }
