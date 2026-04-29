package model

import (
	"encoding/json"
	"time"
)

// Gender represents gender
type Gender string

const (
	GenderMale    Gender = "male"
	GenderFemale  Gender = "female"
	GenderUnknown Gender = "unknown"
)

// AffectedStatus represents affected status
type AffectedStatus string

const (
	AffectedStatusAffected   AffectedStatus = "affected"
	AffectedStatusUnaffected AffectedStatus = "unaffected"
	AffectedStatusUnknown    AffectedStatus = "unknown"
	AffectedStatusCarrier    AffectedStatus = "carrier"
)

// RelationType represents family relationship
type RelationType string

const (
	RelationProband            RelationType = "proband"
	RelationFather             RelationType = "father"
	RelationMother             RelationType = "mother"
	RelationSibling            RelationType = "sibling"
	RelationChild              RelationType = "child"
	RelationSpouse             RelationType = "spouse"
	RelationGrandfatherPaternal RelationType = "grandfather_paternal"
	RelationGrandmotherPaternal RelationType = "grandmother_paternal"
	RelationGrandfatherMaternal RelationType = "grandfather_maternal"
	RelationGrandmotherMaternal RelationType = "grandmother_maternal"
	RelationUncle              RelationType = "uncle"
	RelationAunt               RelationType = "aunt"
	RelationCousin             RelationType = "cousin"
	RelationOther              RelationType = "other"
)

// Pedigree represents a family pedigree
type Pedigree struct {
	ID              string    `json:"id" gorm:"primaryKey;size:36"`
	Name            string    `json:"name" gorm:"size:200;not null"`
	Disease         string    `json:"disease" gorm:"size:200"`
	Note            string    `json:"note" gorm:"type:text"`
	ProbandMemberID string    `json:"proband_member_id" gorm:"size:36;index"`
	CreatedBy       uint      `json:"created_by" gorm:"index"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PedigreeMember represents a member in a pedigree
type PedigreeMember struct {
	ID             string                 `json:"id" gorm:"primaryKey;size:36"`
	PedigreeID     string                 `json:"pedigree_id" gorm:"size:36;index;not null"`
	SampleID       string                 `json:"sample_id,omitempty" gorm:"size:36;index"`
	Name           string                 `json:"name" gorm:"size:100;not null"`
	Gender         Gender                 `json:"gender" gorm:"size:20;default:unknown"`
	BirthYear      *int                   `json:"birth_year,omitempty"`
	IsDeceased     bool                   `json:"is_deceased" gorm:"default:false"`
	DeceasedYear   *int                   `json:"deceased_year,omitempty"`
	Relation       RelationType           `json:"relation" gorm:"size:30;not null"`
	AffectedStatus AffectedStatus         `json:"affected_status" gorm:"size:20;default:unknown"`
	Phenotypes     map[string]interface{} `json:"phenotypes,omitempty" gorm:"type:text"`
	FatherID       string                 `json:"father_id,omitempty" gorm:"size:36;index"`
	MotherID       string                 `json:"mother_id,omitempty" gorm:"size:36;index"`
	Generation     int                    `json:"generation" gorm:"default:0"`
	Position       int                    `json:"position" gorm:"default:0"`
	HasSample      bool                   `json:"has_sample" gorm:"default:false"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// PedigreeResponse is the API response for a pedigree list item
type PedigreeResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Disease     string    `json:"disease,omitempty"`
	MemberCount int       `json:"member_count"`
	CreatedAt   string    `json:"created_at"`
	UpdatedAt   string    `json:"updated_at"`
}

// PedigreeDetailResponse is the API response for a pedigree with members
type PedigreeDetailResponse struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Disease         string                `json:"disease,omitempty"`
	Note            string                `json:"note,omitempty"`
	ProbandMemberID string                `json:"proband_member_id,omitempty"`
	Members         []PedigreeMemberResp  `json:"members"`
	CreatedAt       string                `json:"created_at"`
	UpdatedAt       string                `json:"updated_at"`
}

// PedigreeMemberResp is the API response for a pedigree member
type PedigreeMemberResp struct {
	ID             string                 `json:"id"`
	PedigreeID     string                 `json:"pedigree_id"`
	SampleID       string                 `json:"sample_id,omitempty"`
	Name           string                 `json:"name"`
	Gender         Gender                 `json:"gender"`
	BirthYear      *int                   `json:"birth_year,omitempty"`
	IsDeceased     bool                   `json:"is_deceased"`
	DeceasedYear   *int                   `json:"deceased_year,omitempty"`
	Relation       RelationType           `json:"relation"`
	AffectedStatus AffectedStatus         `json:"affected_status"`
	Phenotypes     map[string]interface{} `json:"phenotypes,omitempty"`
	FatherID       string                 `json:"father_id,omitempty"`
	MotherID       string                 `json:"mother_id,omitempty"`
	Generation     int                    `json:"generation"`
	Position       int                    `json:"position"`
	HasSample      bool                   `json:"has_sample"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
}

// PedigreeCreateRequest is the request body for creating a pedigree
type PedigreeCreateRequest struct {
	Name    string `json:"name" binding:"required"`
	Disease string `json:"disease"`
	Note    string `json:"note"`
}

// PedigreeUpdateRequest is the request body for updating a pedigree
type PedigreeUpdateRequest struct {
	Name    string `json:"name"`
	Disease string `json:"disease"`
	Note    string `json:"note"`
}

// PedigreeMemberCreateRequest is the request body for creating a pedigree member
type PedigreeMemberCreateRequest struct {
	Name           string                 `json:"name" binding:"required"`
	Gender         Gender                 `json:"gender"`
	BirthYear      *int                   `json:"birth_year"`
	IsDeceased     bool                   `json:"is_deceased"`
	DeceasedYear   *int                   `json:"deceased_year"`
	Relation       RelationType           `json:"relation" binding:"required"`
	AffectedStatus AffectedStatus         `json:"affected_status"`
	Phenotypes     map[string]interface{} `json:"phenotypes"`
	FatherID       string                 `json:"father_id"`
	MotherID       string                 `json:"mother_id"`
	Generation     int                    `json:"generation"`
	Position       int                    `json:"position"`
	SampleID       string                 `json:"sample_id"`
}

// PedigreeMemberUpdateRequest is the request body for updating a pedigree member
type PedigreeMemberUpdateRequest struct {
	Name           string                 `json:"name"`
	Gender         Gender                 `json:"gender"`
	BirthYear      *int                   `json:"birth_year"`
	IsDeceased     *bool                  `json:"is_deceased"`
	DeceasedYear   *int                   `json:"deceased_year"`
	Relation       RelationType           `json:"relation"`
	AffectedStatus AffectedStatus         `json:"affected_status"`
	Phenotypes     map[string]interface{} `json:"phenotypes"`
	FatherID       string                 `json:"father_id"`
	MotherID       string                 `json:"mother_id"`
	Generation     *int                   `json:"generation"`
	Position       *int                   `json:"position"`
	SampleID       string                 `json:"sample_id"`
}

// PedigreeListQuery is the query parameters for listing pedigrees
type PedigreeListQuery struct {
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
	Search   string `form:"search"`
}

// PedigreeListResponse is the response for listing pedigrees
type PedigreeListResponse struct {
	Total int                 `json:"total"`
	Items []PedigreeResponse  `json:"items"`
}

// TableName specifies the table name for Pedigree
func (Pedigree) TableName() string {
	return "pedigrees"
}

// TableName specifies the table name for PedigreeMember
func (PedigreeMember) TableName() string {
	return "pedigree_members"
}

// GetPhenotypesJSON returns phenotypes as JSON string for storage
func (m *PedigreeMember) GetPhenotypesJSON() string {
	if m.Phenotypes == nil {
		return ""
	}
	b, _ := json.Marshal(m.Phenotypes)
	return string(b)
}

// SetPhenotypesFromJSON parses JSON string into phenotypes map
func (m *PedigreeMember) SetPhenotypesFromJSON(s string) {
	if s == "" {
		return
	}
	var p map[string]interface{}
	if err := json.Unmarshal([]byte(s), &p); err == nil {
		m.Phenotypes = p
	}
}
