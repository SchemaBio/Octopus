package model

import "time"

const (
	ReferenceGenomeGRCh37 = "GRCh37"
	ReferenceGenomeGRCh38 = "GRCh38"
)

const (
	TaskAssetRoleCNVRead1    = "cnv_baseline_read1"
	TaskAssetRoleCNVRead2    = "cnv_baseline_read2"
	TaskAssetRoleCNVBED      = "cnv_baseline_bed"
	TaskAssetRoleAnalysisBED = "analysis_bed"
)

// CNVBaseline tracks one baseline-generation workflow and its final output.
type CNVBaseline struct {
	ID              uint      `json:"-" gorm:"primaryKey"`
	UUID            string    `json:"id" gorm:"uniqueIndex;size:36;not null"`
	Name            string    `json:"name" gorm:"size:200;not null"`
	ReferenceGenome string    `json:"reference_genome" gorm:"size:20;index;not null"`
	BEDAssetID      uint      `json:"-" gorm:"index;not null"`
	TaskUUID        string    `json:"task_id" gorm:"uniqueIndex;size:36;not null"`
	OutputPath      string    `json:"output_path,omitempty" gorm:"size:1000"`
	InputBytes      int64     `json:"input_bytes" gorm:"not null;default:0"`
	CreditsCharged  int       `json:"credits_charged" gorm:"not null;default:0"`
	ExternalOrgID   string    `json:"-" gorm:"size:100;index"`
	CreatedBy       uint      `json:"-" gorm:"index;not null"`
	CreatedAt       time.Time `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"type:timestamptz"`
}

type CNVBaselineReadPair struct {
	ID           uint `json:"-" gorm:"primaryKey"`
	BaselineID   uint `json:"-" gorm:"uniqueIndex:idx_cnv_baseline_pair;not null"`
	PairIndex    int  `json:"pair_index" gorm:"uniqueIndex:idx_cnv_baseline_pair;not null"`
	Read1AssetID uint `json:"-" gorm:"index;not null"`
	Read2AssetID uint `json:"-" gorm:"index;not null"`
}

// TaskDataAsset persists direct data selections for tasks that do not use a sample.
type TaskDataAsset struct {
	ID         uint   `json:"-" gorm:"primaryKey"`
	TaskUUID   string `json:"-" gorm:"size:36;index;uniqueIndex:idx_task_asset_role;not null"`
	AssetID    uint   `json:"-" gorm:"index;not null"`
	InputRole  string `json:"-" gorm:"size:40;uniqueIndex:idx_task_asset_role;not null"`
	InputIndex int    `json:"-" gorm:"uniqueIndex:idx_task_asset_role;not null"`
}

type TaskInputAssetRequest struct {
	AssetID   uint
	InputRole string
	Index     int
}

type CNVBaselineCreateRequest struct {
	Name            string   `json:"name" binding:"required"`
	ReferenceGenome string   `json:"reference_genome" binding:"required"`
	BEDAssetID      string   `json:"bed_asset_id" binding:"required"`
	Read1AssetIDs   []string `json:"read1_asset_ids" binding:"required,min=1"`
	Read2AssetIDs   []string `json:"read2_asset_ids" binding:"required,min=1"`
}

type CNVBaselineAssetResponse struct {
	ID       string `json:"id"`
	FileName string `json:"file_name"`
}

type CNVBaselineResponse struct {
	ID              string                        `json:"id"`
	Name            string                        `json:"name"`
	ReferenceGenome string                        `json:"reference_genome"`
	BED             CNVBaselineAssetResponse      `json:"bed"`
	ReadPairs       [][2]CNVBaselineAssetResponse `json:"read_pairs"`
	TaskID          string                        `json:"task_id"`
	Status          TaskStatus                    `json:"status"`
	Progress        int                           `json:"progress"`
	OutputPath      string                        `json:"output_path,omitempty"`
	InputBytes      int64                         `json:"input_bytes"`
	CreditCost      int                           `json:"credit_cost"`
	CreditsCharged  int                           `json:"credits_charged"`
	Error           string                        `json:"error,omitempty"`
	CreatedAt       string                        `json:"created_at"`
	UpdatedAt       string                        `json:"updated_at"`
}

func (CNVBaseline) TableName() string         { return "cnv_baselines" }
func (CNVBaselineReadPair) TableName() string { return "cnv_baseline_read_pairs" }
func (TaskDataAsset) TableName() string       { return "task_data_assets" }
