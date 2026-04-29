package model

import "time"

// Organization represents a user organization/workspace
type Organization struct {
	ID          string    `json:"id" gorm:"primaryKey;size:36"`
	Name        string    `json:"name" gorm:"size:100;not null"`
	Slug        string    `json:"slug" gorm:"uniqueIndex;size:100;not null"`
	Description string    `json:"description" gorm:"type:text"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UserOrganization represents the many-to-many relationship between users and organizations
type UserOrganization struct {
	ID     uint   `json:"id" gorm:"primaryKey"`
	UserID uint   `json:"user_id" gorm:"index;not null"`
	OrgID  string `json:"org_id" gorm:"index;size:36;not null"`
	Role   string `json:"role" gorm:"size:20;default:viewer"` // owner, admin, doctor, analyst, viewer
	JoinedAt time.Time `json:"joined_at" gorm:"autoCreateTime"`
}

// OrganizationResponse is the API response for an organization
type OrganizationResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
}

// OrganizationWithRole includes the user's role in the org
type OrganizationWithRole struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	OrgRole     string `json:"org_role"`
	JoinedAt    string `json:"joined_at"`
}

// SwitchOrgRequest is the request body for switching organization
type SwitchOrgRequest struct {
	OrgID string `json:"org_id" binding:"required"`
}

// OrgListResponse is the response for listing organizations
type OrgListResponse struct {
	Organizations []OrganizationWithRole `json:"organizations"`
}

// TableName specifies the table name for Organization
func (Organization) TableName() string {
	return "organizations"
}

// TableName specifies the table name for UserOrganization
func (UserOrganization) TableName() string {
	return "user_organizations"
}
