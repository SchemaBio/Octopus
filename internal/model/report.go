package model

import "time"

// ReportStatus represents the status of a report
type ReportStatus string

const (
	ReportStatusDraft         ReportStatus = "DRAFT"
	ReportStatusPendingReview ReportStatus = "PENDING_REVIEW"
	ReportStatusApproved      ReportStatus = "APPROVED"
	ReportStatusReleased      ReportStatus = "RELEASED"
)

// Report represents a legacy generated report record. New report generation is
// downloaded directly from the configured report API and is not persisted here.
type Report struct {
	ID             string       `json:"id" gorm:"primaryKey;size:36"`
	TaskID         string       `json:"taskId" gorm:"size:36;index"`
	Name           string       `json:"name" gorm:"size:200"`
	Type           string       `json:"type" gorm:"size:20"` // legacy generated/uploaded records
	TemplateName   string       `json:"templateName" gorm:"size:200"`
	FileName       string       `json:"fileName" gorm:"size:500"`
	ExternalURL    string       `json:"externalUrl" gorm:"size:500"`
	UploadedFileID *uint        `json:"uploadedFileId,omitempty" gorm:"index"`
	Status         ReportStatus `json:"status" gorm:"size:30;default:DRAFT"`
	ExternalJobID  string       `json:"externalJobId" gorm:"size:100"`
	CreatedBy      string       `json:"createdBy" gorm:"size:100"`
	ReviewedBy     string       `json:"reviewedBy,omitempty" gorm:"size:100"`
	ApprovedBy     string       `json:"approvedBy,omitempty" gorm:"size:100"`
	ReleasedBy     string       `json:"releasedBy,omitempty" gorm:"size:100"`
	CreatedAt      time.Time    `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt      time.Time    `json:"updated_at" gorm:"type:timestamptz"`
}

func (Report) TableName() string {
	return "reports"
}

// ReportTemplate represents an available report generation template/API
type ReportTemplate struct {
	ExternalOrgID   string    `json:"-" gorm:"size:100;not null;default:'';index"`
	ContractVersion string    `json:"contractVersion" gorm:"size:40;not null;default:'legacy-v1'"`
	Revision        uint64    `json:"revision" gorm:"not null;default:1"`
	ID              string    `json:"id" gorm:"primaryKey;size:36"`
	OwnerUserID     uint      `json:"-" gorm:"index:idx_report_templates_owner_name,unique"`
	Name            string    `json:"name" gorm:"size:200;not null"`
	Description     string    `json:"description" gorm:"type:text"`
	APIEndpoint     string    `json:"apiEndpoint" gorm:"size:500;not null"` // User's report generation API
	APIKey          string    `json:"-" gorm:"size:500;not null"`
	IsActive        bool      `json:"isActive" gorm:"default:true"`
	CreatedAt       time.Time `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"type:timestamptz"`
}

func (ReportTemplate) TableName() string {
	return "report_templates"
}

// ReportCreateRequest is the request for creating a report
type ReportCreateRequest struct {
	GenerationID    string `json:"-"`
	SnapshotJSON    string `json:"-"`
	SnapshotSHA256  string `json:"-"`
	ClientRequestID string `json:"clientRequestId"`
	Name            string `json:"name" binding:"required"`
	TemplateID      string `json:"templateId"`
	TemplateName    string `json:"templateName"`
}

// ReportUploadRequest is a legacy request shape. Uploaded reports are disabled.
type ReportUploadRequest struct {
	Name           string `json:"name" binding:"required"`
	FileName       string `json:"fileName"`
	UploadedFileID uint   `json:"uploadedFileId" binding:"required"`
}

// ReportStatusUpdateRequest changes a report workflow status.
type ReportStatusUpdateRequest struct {
	Status ReportStatus `json:"status" binding:"required"`
}

// ReportResponse is the API response for a report
type ReportResponse struct {
	ID             string       `json:"id"`
	TaskID         string       `json:"taskId"`
	Name           string       `json:"name"`
	Type           string       `json:"type"`
	TemplateName   string       `json:"templateName,omitempty"`
	FileName       string       `json:"fileName,omitempty"`
	ExternalURL    string       `json:"externalUrl,omitempty"`
	UploadedFileID *uint        `json:"uploadedFileId,omitempty"`
	Status         ReportStatus `json:"status"`
	CreatedBy      string       `json:"createdBy"`
	ReviewedBy     string       `json:"reviewedBy,omitempty"`
	ApprovedBy     string       `json:"approvedBy,omitempty"`
	ReleasedBy     string       `json:"releasedBy,omitempty"`
	CreatedAt      string       `json:"created_at"`
}

func (r *Report) ToResponse() ReportResponse {
	return ReportResponse{
		ID:             r.ID,
		TaskID:         r.TaskID,
		Name:           r.Name,
		Type:           r.Type,
		TemplateName:   r.TemplateName,
		FileName:       r.FileName,
		ExternalURL:    r.ExternalURL,
		UploadedFileID: r.UploadedFileID,
		Status:         r.Status,
		CreatedBy:      r.CreatedBy,
		ReviewedBy:     r.ReviewedBy,
		ApprovedBy:     r.ApprovedBy,
		ReleasedBy:     r.ReleasedBy,
		CreatedAt:      r.CreatedAt.Format(time.RFC3339),
	}
}

// ReportTemplateCreateRequest is the request for creating a report template
type ReportTemplateCreateRequest struct {
	ContractVersion string `json:"contractVersion"`
	Name            string `json:"name" binding:"required"`
	Description     string `json:"description"`
	APIEndpoint     string `json:"apiEndpoint" binding:"required"`
	APIKey          string `json:"apiKey"`
}

// ReportTemplateUpdateRequest is the request for updating a report template.
// APIKey is optional and only rotated when present and non-empty.
type ReportTemplateUpdateRequest struct {
	ContractVersion  string `json:"contractVersion"`
	ExpectedRevision uint64 `json:"expectedRevision"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	APIEndpoint      string `json:"apiEndpoint"`
	APIKey           string `json:"apiKey"`
	IsActive         *bool  `json:"isActive"`
}

// ReportTemplateStatusRequest toggles template availability.
type ReportTemplateStatusRequest struct {
	IsActive bool `json:"isActive"`
}

type ReportEndpointValidateRequest struct {
	APIEndpoint string `json:"apiEndpoint" binding:"required"`
	APIKey      string `json:"apiKey"`
	TemplateID  string `json:"templateId"`
}

// ReportTemplateResponse is the public-safe template payload (omits APIEndpoint and APIKey)
type ReportTemplateResponse struct {
	ContractVersion string `json:"contractVersion"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	IsActive        bool   `json:"isActive"`
	CreatedAt       string `json:"createdAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

// ReportTemplateAdminResponse is the template payload for admins.
type ReportTemplateAdminResponse struct {
	ContractVersion string `json:"contractVersion"`
	Revision        uint64 `json:"revision"`
	Scope           string `json:"scope"`
	CanMaintain     bool   `json:"canMaintain"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	APIEndpoint     string `json:"apiEndpoint"`
	HasAPIKey       bool   `json:"hasApiKey"`
	IsActive        bool   `json:"isActive"`
	CreatedAt       string `json:"createdAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

// ToResponse converts a ReportTemplate to a public-safe response (omits sensitive fields)
func (t *ReportTemplate) ToResponse() ReportTemplateResponse {
	return ReportTemplateResponse{
		ContractVersion: t.ContractVersion,
		ID:              t.ID,
		Name:            t.Name,
		Description:     t.Description,
		IsActive:        t.IsActive,
		CreatedAt:       t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       t.UpdatedAt.Format(time.RFC3339),
	}
}

func (t *ReportTemplate) ToAdminResponse() ReportTemplateAdminResponse {
	return ReportTemplateAdminResponse{
		ContractVersion: t.ContractVersion, Revision: t.Revision, Scope: resourceScope(t.ExternalOrgID),
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		APIEndpoint: t.APIEndpoint,
		HasAPIKey:   t.APIKey != "",
		IsActive:    t.IsActive,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt.Format(time.RFC3339),
	}
}

// ReportGeneration holds an immutable report input snapshot and an idempotent
// external invocation. Stored file/object locations never leave this service.
type ReportGeneration struct {
	ID              string    `json:"id" gorm:"primaryKey;size:36"`
	TenantID        string    `json:"-" gorm:"size:160;uniqueIndex:idx_report_request,priority:1"`
	TaskUUID        string    `json:"taskUuid" gorm:"size:36;index"`
	AttemptID       string    `json:"attemptId" gorm:"size:36"`
	ClientRequestID string    `json:"-" gorm:"size:36;uniqueIndex:idx_report_request,priority:2"`
	TemplateID      string    `json:"templateId" gorm:"size:36"`
	ServiceRevision uint64    `json:"serviceRevision"`
	ContractVersion string    `json:"contractVersion" gorm:"size:40"`
	SnapshotSHA256  string    `json:"snapshotSha256" gorm:"size:64"`
	SnapshotJSON    string    `json:"-" gorm:"type:jsonb;not null"`
	State           string    `json:"state" gorm:"size:20"`
	ErrorCode       string    `json:"errorCode" gorm:"size:80"`
	FileName        string    `json:"fileName" gorm:"size:500"`
	ContentType     string    `json:"contentType" gorm:"size:200"`
	ObjectKey       string    `json:"-" gorm:"type:text"`
	CreatedBy       uint      `json:"createdBy"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
