package model

import (
	"encoding/json"
	"time"
)

// GeneListCategory represents the category of a gene list
type GeneListCategory string

const (
	GeneListCategoryCore     GeneListCategory = "core"
	GeneListCategoryImportant GeneListCategory = "important"
	GeneListCategoryOptional GeneListCategory = "optional"
)

// GeneList represents a gene panel/list
type GeneList struct {
	ID              string           `json:"id" gorm:"primaryKey;size:36"`
	Name            string           `json:"name" gorm:"size:200;not null"`
	Description     string           `json:"description" gorm:"type:text"`
	GenesJSON       string           `json:"-" gorm:"type:text"` // JSON array stored in DB
	Category        GeneListCategory `json:"category" gorm:"size:20"`
	DiseaseCategory string           `json:"disease_category" gorm:"size:100"`
	CreatedBy       uint             `json:"created_by" gorm:"index"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// GeneListResponse is the API response for a gene list
type GeneListResponse struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description,omitempty"`
	Genes           []string         `json:"genes"`
	GeneCount       int              `json:"gene_count"`
	Category        GeneListCategory `json:"category,omitempty"`
	DiseaseCategory string           `json:"disease_category,omitempty"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
}

// GeneListCreateRequest is the request body for creating a gene list
type GeneListCreateRequest struct {
	Name            string           `json:"name" binding:"required"`
	Description     string           `json:"description"`
	Genes           []string         `json:"genes" binding:"required"`
	Category        GeneListCategory `json:"category"`
	DiseaseCategory string           `json:"disease_category"`
}

// GeneListUpdateRequest is the request body for updating a gene list
type GeneListUpdateRequest struct {
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	Genes           []string         `json:"genes"`
	Category        GeneListCategory `json:"category"`
	DiseaseCategory string           `json:"disease_category"`
}

// GeneListListQuery is the query parameters for listing gene lists
type GeneListListQuery struct {
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
	Search   string `form:"search"`
}

// GeneListListResponse is the response for listing gene lists
type GeneListListResponse struct {
	Total int                `json:"total"`
	Items []GeneListResponse `json:"items"`
}

// TableName specifies the table name for GeneList
func (GeneList) TableName() string {
	return "gene_lists"
}

// GetGenes parses GenesJSON into a string slice
func (g *GeneList) GetGenes() []string {
	if g.GenesJSON == "" {
		return []string{}
	}
	var genes []string
	json.Unmarshal([]byte(g.GenesJSON), &genes)
	return genes
}

// SetGenes sets GenesJSON from a string slice
func (g *GeneList) SetGenes(genes []string) {
	b, _ := json.Marshal(genes)
	g.GenesJSON = string(b)
}

// ToResponse converts GeneList to GeneListResponse
func (g *GeneList) ToResponse() GeneListResponse {
	genes := g.GetGenes()
	return GeneListResponse{
		ID:              g.ID,
		Name:            g.Name,
		Description:     g.Description,
		Genes:           genes,
		GeneCount:       len(genes),
		Category:        g.Category,
		DiseaseCategory: g.DiseaseCategory,
		CreatedAt:       g.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       g.UpdatedAt.Format(time.RFC3339),
	}
}
