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
	PipelineBasePanel     PipelineBaseType = "panel"
)

// Pipeline represents an analysis pipeline configuration
type Pipeline struct {
	ID               string           `json:"id" gorm:"primaryKey;size:36"`
	Name             string           `json:"name" gorm:"size:200;not null"`
	BaseType         PipelineBaseType `json:"base_type" gorm:"size:30"`
	Version          string           `json:"version" gorm:"size:50"`
	Description      string           `json:"description" gorm:"type:text"`
	BEDFile          string           `json:"bed_file" gorm:"size:500"`
	ReferenceGenome  string           `json:"reference_genome" gorm:"size:20"` // hg19, hg38
	CNVBaseline      string           `json:"cnv_baseline" gorm:"size:500"`
	Status           PipelineStatus   `json:"status" gorm:"size:20;default:active"`
	CreatedBy        uint             `json:"created_by" gorm:"index"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// PipelineResponse is the API response for a pipeline
type PipelineResponse struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	BaseType        PipelineBaseType `json:"base_type"`
	Version         string           `json:"version"`
	Description     string           `json:"description,omitempty"`
	BEDFile         string           `json:"bed_file,omitempty"`
	ReferenceGenome string           `json:"reference_genome,omitempty"`
	CNVBaseline     string           `json:"cnv_baseline,omitempty"`
	Status          PipelineStatus   `json:"status"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
}

// PipelineCreateRequest is the request body for creating a pipeline
type PipelineCreateRequest struct {
	Name            string           `json:"name" binding:"required"`
	BaseType        PipelineBaseType `json:"base_type"`
	Version         string           `json:"version"`
	Description     string           `json:"description"`
	BEDFile         string           `json:"bed_file"`
	ReferenceGenome string           `json:"reference_genome"`
	CNVBaseline     string           `json:"cnv_baseline"`
}

// PipelineUpdateRequest is the request body for updating a pipeline
type PipelineUpdateRequest struct {
	Name            string           `json:"name"`
	BaseType        PipelineBaseType `json:"base_type"`
	Version         string           `json:"version"`
	Description     string           `json:"description"`
	BEDFile         string           `json:"bed_file"`
	ReferenceGenome string           `json:"reference_genome"`
	CNVBaseline     string           `json:"cnv_baseline"`
	Status          PipelineStatus   `json:"status"`
}

// PipelineListQuery is the query parameters for listing pipelines
type PipelineListQuery struct {
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
	Search   string `form:"search"`
}

// PipelineListResponse is the response for listing pipelines
type PipelineListResponse struct {
	Total int                 `json:"total"`
	Items []PipelineResponse  `json:"items"`
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
		BaseType:        p.BaseType,
		Version:         p.Version,
		Description:     p.Description,
		BEDFile:         p.BEDFile,
		ReferenceGenome: p.ReferenceGenome,
		CNVBaseline:     p.CNVBaseline,
		Status:          p.Status,
		CreatedAt:       p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       p.UpdatedAt.Format(time.RFC3339),
	}
}
