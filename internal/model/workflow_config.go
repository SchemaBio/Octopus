package model

import "time"

// WorkflowThresholdConfig stores SaaS administrator overrides for the small
// set of tunable germline calling thresholds. Workflow resources remain in
// the code-owned catalog.
type WorkflowThresholdConfig struct {
	SexMinSites     int       `json:"sex_min_sites" gorm:"not null;default:100"`
	FemaleMaxRatio  float64   `json:"female_max_ratio" gorm:"not null;default:0"`
	MaleMinRatio    float64   `json:"male_min_ratio" gorm:"not null;default:0.7"`
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
	SexMinSites     int     `json:"sex_min_sites"`
	FemaleMaxRatio  float64 `json:"female_max_ratio"`
	MaleMinRatio    float64 `json:"male_min_ratio"`
	SRYSexCutoff    float64 `json:"sry_sex_cutoff" binding:"gte=0"`
	CNVBinSize      int     `json:"cnv_bin_size" binding:"gte=0"`
	CNVDupThreshold float64 `json:"cnv_dup_threshold" binding:"gte=0"`
	CNVDelThreshold float64 `json:"cnv_del_threshold" binding:"gte=0"`
}

func (WorkflowThresholdConfig) TableName() string { return "workflow_threshold_configs" }
