package model

import "time"

// PipelineStatus represents the status of a pipeline
type PipelineStatus string

const (
	PipelineStatusActive   PipelineStatus = "active"
	PipelineStatusInactive PipelineStatus = "inactive"
)

// PipelineBaseType represents the base pipeline type
type PipelineBaseType string

const (
	PipelineBaseWESSingle PipelineBaseType = "wes_single"
	PipelineBaseWESFamily PipelineBaseType = "wes_family"
)

const (
	BuiltinPipelineWESSingleID     = "builtin-wes-single"
	BuiltinPipelineWESFamilyID     = "builtin-wes-family"
	BuiltinPipelineWESSingleHG38ID = "builtin-wes-single-hg38"
	BuiltinPipelineWESFamilyHG38ID = "builtin-wes-family-hg38"
	BuiltinBEDHG19ID               = "builtin-bed-hg19"
	BuiltinBEDHG38ID               = "builtin-bed-hg38"
	BuiltinCNVBaselineHG19ID       = "builtin-cnv-baseline-hg19"
	BuiltinCNVBaselineHG38ID       = "builtin-cnv-baseline-hg38"
)

// Pipeline represents an analysis pipeline configuration
type Pipeline struct {
	ID              string           `json:"id" gorm:"primaryKey;size:36"`
	Name            string           `json:"name" gorm:"size:200;not null"`
	BaseType        PipelineBaseType `json:"base_type" gorm:"size:30"`
	Version         string           `json:"version" gorm:"size:50"`
	Description     string           `json:"description" gorm:"type:text"`
	BEDFile         string           `json:"bed_file" gorm:"size:500"`
	BEDAssetID      *uint            `json:"-" gorm:"index"`
	ReferenceGenome string           `json:"reference_genome" gorm:"size:20"` // hg19, hg38
	CNVBaseline     string           `json:"cnv_baseline" gorm:"size:500"`
	CNVBaselineID   *uint            `json:"-" gorm:"index"`
	Status          PipelineStatus   `json:"status" gorm:"size:20;default:active"`
	ExternalOrgID   string           `json:"-" gorm:"size:100;index"`
	CreatedBy       uint             `json:"created_by" gorm:"index"`
	CreatedAt       time.Time        `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt       time.Time        `json:"updated_at" gorm:"type:timestamptz"`
}

// PipelineResponse is the API response for a pipeline
type PipelineResponse struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	BasePipelineID  string           `json:"base_pipeline_id"`
	BaseType        PipelineBaseType `json:"base_type"`
	Version         string           `json:"version"`
	Description     string           `json:"description,omitempty"`
	BEDFile         string           `json:"bed_file,omitempty"`
	ReferenceGenome string           `json:"reference_genome,omitempty"`
	CNVBaseline     string           `json:"cnv_baseline,omitempty"`
	BEDAssetID      string           `json:"bed_asset_id,omitempty"`
	CNVBaselineID   string           `json:"cnv_baseline_id,omitempty"`
	Template        string           `json:"template"`
	IsBuiltin       bool             `json:"is_builtin"`
	Status          PipelineStatus   `json:"status"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
}

// PipelineCreateRequest is the request body for creating a pipeline
type PipelineCreateRequest struct {
	Name            string           `json:"name" binding:"required"`
	BasePipelineID  string           `json:"base_pipeline_id"`
	BaseType        PipelineBaseType `json:"base_type"`
	Version         string           `json:"version"`
	Description     string           `json:"description"`
	BEDFile         string           `json:"bed_file"`
	ReferenceGenome string           `json:"reference_genome"`
	CNVBaseline     string           `json:"cnv_baseline"`
	BEDAssetID      string           `json:"bed_asset_id"`
	CNVBaselineID   string           `json:"cnv_baseline_id"`
}

// PipelineUpdateRequest is the request body for updating a pipeline
type PipelineUpdateRequest struct {
	Name            string           `json:"name"`
	BasePipelineID  string           `json:"base_pipeline_id"`
	BaseType        PipelineBaseType `json:"base_type"`
	Version         string           `json:"version"`
	Description     string           `json:"description"`
	BEDFile         string           `json:"bed_file"`
	ReferenceGenome string           `json:"reference_genome"`
	CNVBaseline     string           `json:"cnv_baseline"`
	BEDAssetID      string           `json:"bed_asset_id"`
	CNVBaselineID   string           `json:"cnv_baseline_id"`
	Status          PipelineStatus   `json:"status"`
}

// PipelineListQuery is the query parameters for listing pipelines
type PipelineListQuery struct {
	Page          int    `form:"page" binding:"omitempty,min=1"`
	PageSize      int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Search        string `form:"search"`
	CreatedBy     uint   `json:"-"`
	ExternalOrgID string `json:"-"`
	IncludeAll    bool   `json:"-"`
}

// PipelineListResponse is the response for listing pipelines
type PipelineListResponse struct {
	Total int64              `json:"total"`
	Items []PipelineResponse `json:"items"`
}

// TableName specifies the table name for Pipeline
func (Pipeline) TableName() string {
	return "pipelines"
}

// ToResponse converts Pipeline to PipelineResponse
func (p *Pipeline) ToResponse() PipelineResponse {
	return PipelineResponse{
		ID:              p.ID,
		Name:            p.Name,
		BasePipelineID:  BuiltinPipelineID(p.BaseType, p.ReferenceGenome),
		BaseType:        p.BaseType,
		Version:         p.Version,
		Description:     p.Description,
		BEDFile:         p.BEDFile,
		ReferenceGenome: p.ReferenceGenome,
		CNVBaseline:     p.CNVBaseline,
		Template:        PipelineTemplate(p.BaseType),
		Status:          p.Status,
		CreatedAt:       p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       p.UpdatedAt.Format(time.RFC3339),
	}
}

func BuiltinPipelineID(baseType PipelineBaseType, genome string) string {
	if genome == "hg38" {
		if baseType == PipelineBaseWESFamily {
			return BuiltinPipelineWESFamilyHG38ID
		}
		return BuiltinPipelineWESSingleHG38ID
	}
	if baseType == PipelineBaseWESFamily {
		return BuiltinPipelineWESFamilyID
	}
	return BuiltinPipelineWESSingleID
}

func PipelineTemplate(baseType PipelineBaseType) string {
	if baseType == PipelineBaseWESFamily {
		return "trio"
	}
	return "single"
}

func IsBuiltinPipelineID(id string) bool {
	switch id {
	case BuiltinPipelineWESSingleID, BuiltinPipelineWESFamilyID,
		BuiltinPipelineWESSingleHG38ID, BuiltinPipelineWESFamilyHG38ID:
		return true
	default:
		return false
	}
}

func BuiltinBEDResourceID(genome string) string {
	if genome == "hg38" {
		return BuiltinBEDHG38ID
	}
	return BuiltinBEDHG19ID
}

func BuiltinCNVBaselineResourceID(genome string) string {
	if genome == "hg38" {
		return BuiltinCNVBaselineHG38ID
	}
	return BuiltinCNVBaselineHG19ID
}
