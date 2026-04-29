package model

import (
	"strconv"
	"time"
)

// SystemRole represents the system-level role of a user
type SystemRole string

const (
	SystemRoleSuperAdmin SystemRole = "SUPER_ADMIN"
	SystemRoleUser       SystemRole = "USER"
)

// User represents a user account
type User struct {
	ID           uint       `json:"id" gorm:"primaryKey"`
	Username     string     `json:"-" gorm:"uniqueIndex;size:50;not null"` // internal use only, not exposed in API
	Password     string     `json:"-" gorm:"size:255;not null"`
	Email        string     `json:"email" gorm:"uniqueIndex;size:100;not null"`
	Name         string     `json:"name" gorm:"size:100;not null"`
	SystemRole   SystemRole `json:"system_role" gorm:"size:20;default:USER"`
	PrimaryOrgID string     `json:"primary_org_id,omitempty" gorm:"size:36;index"`
	IsActive     bool       `json:"is_active" gorm:"default:true"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// LoginRequest represents login request body (frontend uses email)
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// UserResponse is the user object returned in API responses
type UserResponse struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	SystemRole   SystemRole `json:"system_role"`
	PrimaryOrgID string     `json:"primary_org_id,omitempty"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

// OrganizationInfo is a minimal org representation for auth responses
type OrganizationInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	OrgRole     string `json:"org_role"`
	JoinedAt    string `json:"joined_at"`
}

// LoginResponse represents login response (matches frontend LoginResponse)
type LoginResponse struct {
	User          UserResponse       `json:"user"`
	Organizations []OrganizationInfo `json:"organizations"`
	CurrentOrg    *OrganizationInfo  `json:"current_org,omitempty"`
	AccessToken   string             `json:"access_token"`
	RefreshToken  string             `json:"refresh_token"`
	ExpiresAt     string             `json:"expires_at"`
}

// RegisterRequest represents register request body
type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
}

// RefreshRequest represents refresh token request
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// RefreshResponse represents refresh token response
type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

// UserCreateRequest is the request body for creating a user (admin)
type UserCreateRequest struct {
	Email        string     `json:"email" binding:"required,email"`
	Name         string     `json:"name" binding:"required"`
	Password     string     `json:"password" binding:"required,min=6"`
	SystemRole   SystemRole `json:"system_role"`
	PrimaryOrgID string     `json:"primary_org_id"`
}

// UserUpdateRequest is the request body for updating a user
type UserUpdateRequest struct {
	Name         string     `json:"name"`
	SystemRole   SystemRole `json:"system_role"`
	PrimaryOrgID string     `json:"primary_org_id"`
}

// UserListQuery is the query parameters for listing users
type UserListQuery struct {
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
	Search   string `form:"search"`
}

// UserListResponse is the response for listing users
type UserListResponse struct {
	Total int           `json:"total"`
	Items []UserResponse `json:"items"`
}

// UserToResponse converts a User model to UserResponse
func UserToResponse(u *User) UserResponse {
	return UserResponse{
		ID:           formatID(u.ID),
		Email:        u.Email,
		Name:         u.Name,
		SystemRole:   u.SystemRole,
		PrimaryOrgID: u.PrimaryOrgID,
		IsActive:     u.IsActive,
		CreatedAt:    u.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    u.UpdatedAt.Format(time.RFC3339),
	}
}

// formatID converts a uint ID to string
func formatID(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
