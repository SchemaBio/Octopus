package model

import "time"

// ReportStatus represents the status of a report
type ReportStatus string

const (
	ReportStatusDraft          ReportStatus = "DRAFT"
	ReportStatusPendingReview  ReportStatus = "PENDING_REVIEW"
	ReportStatusApproved       ReportStatus = "APPROVED"
	ReportStatusReleased       ReportStatus = "RELEASED"
)

// Report represents a generated report record
type Report struct {
	ID             string       `json:"id" gorm:"primaryKey;size:36"`
	TaskID         string       `json:"taskId" gorm:"size:36;index"`
	Name           string       `json:"name" gorm:"size:200"`
	Type           string       `json:"type" gorm:"size:20"` // generated, uploaded
	TemplateName   string       `json:"templateName" gorm:"size:200"`
	FileName       string       `json:"fileName" gorm:"size:500"`
	ExternalURL    string       `json:"externalUrl" gorm:"size:500"`
	Status         ReportStatus `json:"status" gorm:"size:30;default:DRAFT"`
	ExternalJobID  string       `json:"externalJobId" gorm:"size:100"`
	CreatedBy      string       `json:"createdBy" gorm:"size:100"`
	ReviewedBy     string       `json:"reviewedBy,omitempty" gorm:"size:100"`
	ApprovedBy     string       `json:"approvedBy,omitempty" gorm:"size:100"`
	ReleasedBy     string       `json:"releasedBy,omitempty" gorm:"size:100"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

func (Report) TableName() string {
	return "reports"
}

// ReportTemplate represents an available report generation template/API
type ReportTemplate struct {
	ID          string `json:"id" gorm:"primaryKey;size:36"`
	Name        string `json:"name" gorm:"size:200;not null"`
	Description string `json:"description" gorm:"type:text"`
	APIEndpoint string `json:"apiEndpoint" gorm:"size:500;not null"` // User's report generation API
	APIKey      string `json:"apiKey,omitempty" gorm:"size:500"`
	IsActive    bool   `json:"isActive" gorm:"default:true"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ReportTemplate) TableName() string {
	return "report_templates"
}

// ReportCreateRequest is the request for creating a report
type ReportCreateRequest struct {
	Name         string `json:"name" binding:"required"`
	TemplateName string `json:"templateName"`
}

// ReportResponse is the API response for a report
type ReportResponse struct {
	ID           string       `json:"id"`
	TaskID       string       `json:"taskId"`
	Name         string       `json:"name"`
	Type         string       `json:"type"`
	TemplateName string       `json:"templateName,omitempty"`
	FileName     string       `json:"fileName,omitempty"`
	ExternalURL  string       `json:"externalUrl,omitempty"`
	Status       ReportStatus `json:"status"`
	CreatedBy    string       `json:"createdBy"`
	ReviewedBy   string       `json:"reviewedBy,omitempty"`
	ApprovedBy   string       `json:"approvedBy,omitempty"`
	ReleasedBy   string       `json:"releasedBy,omitempty"`
	CreatedAt    string       `json:"created_at"`
}

func (r *Report) ToResponse() ReportResponse {
	return ReportResponse{
		ID:           r.ID,
		TaskID:       r.TaskID,
		Name:         r.Name,
		Type:         r.Type,
		TemplateName: r.TemplateName,
		FileName:     r.FileName,
		ExternalURL:  r.ExternalURL,
		Status:       r.Status,
		CreatedBy:    r.CreatedBy,
		ReviewedBy:   r.ReviewedBy,
		ApprovedBy:   r.ApprovedBy,
		ReleasedBy:   r.ReleasedBy,
		CreatedAt:    r.CreatedAt.Format(time.RFC3339),
	}
}

// ReportTemplateCreateRequest is the request for creating a report template
type ReportTemplateCreateRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	APIEndpoint string `json:"apiEndpoint" binding:"required"`
	APIKey      string `json:"apiKey"`
}
