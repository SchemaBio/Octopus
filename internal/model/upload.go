package model

import (
	"time"
)

type UploadFileType string

const (
	UploadFileTypeFastqPaired UploadFileType = "fastq_paired"
	UploadFileTypeFastqSingle UploadFileType = "fastq_single"
	UploadFileTypeBed         UploadFileType = "bed"
	UploadFileTypeOther       UploadFileType = "other"
)

type UploadProvider string

const (
	UploadProviderLocal UploadProvider = "local"
)

type UploadJobStatus string

const (
	UploadJobStatusPending   UploadJobStatus = "pending"
	UploadJobStatusUploading UploadJobStatus = "uploading"
	UploadJobStatusCompleted UploadJobStatus = "completed"
	UploadJobStatusFailed    UploadJobStatus = "failed"
)

type ReadType string

const (
	ReadTypeRead1  ReadType = "read1"
	ReadTypeRead2  ReadType = "read2"
	ReadTypeSingle ReadType = "single"
	ReadTypeBed    ReadType = "bed"
)

type FileStatus string

const (
	FileStatusPending   FileStatus = "pending"
	FileStatusUploading FileStatus = "uploading"
	FileStatusCompleted FileStatus = "completed"
	FileStatusFailed    FileStatus = "failed"
)

type UploadJob struct {
	ID            uint            `json:"-" gorm:"primaryKey"`
	UUID          string          `json:"id" gorm:"uniqueIndex;size:36;not null"`
	UserID        uint            `json:"user_id" gorm:"index;not null"`
	ExternalOrgID string          `json:"-" gorm:"size:100;index"` // optional external tenant reference (mirrors Task.ExternalOrgID)
	SampleID      string          `json:"sample_id" gorm:"size:36;index"`
	Name          string          `json:"name" gorm:"size:255;not null"`
	FileType      UploadFileType  `json:"file_type" gorm:"size:50;not null"`
	Provider      UploadProvider  `json:"provider" gorm:"size:20;not null;default:local"`
	Status        UploadJobStatus `json:"status" gorm:"size:20;not null;default:pending"`
	CreatedAt     time.Time       `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt     time.Time       `json:"updated_at" gorm:"type:timestamptz"`
}

type UploadFile struct {
	ID         uint       `json:"-" gorm:"primaryKey"`
	UUID       string     `json:"id" gorm:"uniqueIndex;size:36;not null"`
	JobID      uint       `json:"-" gorm:"index;not null"`
	JobUUID    string     `json:"job_id" gorm:"size:36;index"`
	FileName   string     `json:"file_name" gorm:"size:500;not null"`
	StorageKey string     `json:"storage_key" gorm:"size:1000;not null"`
	FileSize   int64      `json:"file_size" gorm:"default:0"`
	ReadType   ReadType   `json:"read_type" gorm:"size:20;not null"`
	Status     FileStatus `json:"status" gorm:"size:20;not null;default:pending"`
	CreatedAt  time.Time  `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt  time.Time  `json:"updated_at" gorm:"type:timestamptz"`
}

type UploadJobCreateRequest struct {
	SampleID string                 `json:"sample_id"`
	Name     string                 `json:"name" binding:"required"`
	FileType UploadFileType         `json:"file_type" binding:"required"`
	Provider UploadProvider         `json:"provider"`
	Files    []UploadFileCreateItem `json:"files" binding:"required,min=1,dive"`
}

type UploadFileCreateItem struct {
	FileName string   `json:"file_name" binding:"required"`
	ReadType ReadType `json:"read_type" binding:"required"`
	FileSize int64    `json:"file_size"`
}

type UploadJobResponse struct {
	ID        string               `json:"id"`
	UserID    uint                 `json:"user_id"`
	SampleID  string               `json:"sample_id,omitempty"`
	Name      string               `json:"name"`
	FileType  UploadFileType       `json:"file_type"`
	Provider  UploadProvider       `json:"provider"`
	Status    UploadJobStatus      `json:"status"`
	Files     []UploadFileResponse `json:"files,omitempty"`
	CreatedAt string               `json:"created_at"`
	UpdatedAt string               `json:"updated_at"`
}

type UploadFileResponse struct {
	ID           string     `json:"id"`
	JobID        string     `json:"job_id"`
	FileName     string     `json:"file_name"`
	StorageKey   string     `json:"-"` // server-local path/storage key must never be serialized to browsers
	FileSize     int64      `json:"file_size"`
	ReadType     ReadType   `json:"read_type"`
	Status       FileStatus `json:"status"`
	PresignedURL string     `json:"presigned_url,omitempty"`
	CreatedAt    string     `json:"created_at"`
}

type UploadJobListQuery struct {
	Page     int             `form:"page" binding:"min=1"`
	PageSize int             `form:"page_size" binding:"min=1,max=100"`
	Status   UploadJobStatus `form:"status"`
	FileType UploadFileType  `form:"file_type"`
}

type UploadJobListResponse struct {
	Total int64               `json:"total"`
	Items []UploadJobResponse `json:"items"`
}

type UploadFileCompleteRequest struct {
	FileSize int64 `json:"file_size"`
}

func UploadJobToResponse(job *UploadJob, files []UploadFileResponse) UploadJobResponse {
	return UploadJobResponse{
		ID:        job.UUID,
		UserID:    job.UserID,
		SampleID:  job.SampleID,
		Name:      job.Name,
		FileType:  job.FileType,
		Provider:  job.Provider,
		Status:    job.Status,
		Files:     files,
		CreatedAt: job.CreatedAt.Format(time.RFC3339),
		UpdatedAt: job.UpdatedAt.Format(time.RFC3339),
	}
}

func UploadFileToResponse(file *UploadFile) UploadFileResponse {
	return UploadFileResponse{
		ID:        file.UUID,
		JobID:     file.JobUUID,
		FileName:  file.FileName,
		FileSize:  file.FileSize,
		ReadType:  file.ReadType,
		Status:    file.Status,
		CreatedAt: file.CreatedAt.Format(time.RFC3339),
	}
}

// UploadFileListQuery is the query parameters for the file-level audit list
// endpoint (GET /upload/files). OrgID is an explicit admin cross-org filter;
// the internal scope fields (UserID/ExternalOrgID/IncludeAll) are set by the
// handler from the caller identity, mirroring TaskListQuery scoping.
type UploadFileListQuery struct {
	Page     int        `form:"page" binding:"min=1"`
	PageSize int        `form:"page_size" binding:"min=1,max=100"`
	Status   FileStatus `form:"status"`
	Search   string     `form:"search"` // matches file_name ILIKE
	OrgID    string     `form:"org_id"` // admin cross-org filter
	// internal scope (set by handler, not bound from query):
	UserID        uint   `json:"-"`
	ExternalOrgID string `json:"-"`
	IncludeAll    bool   `json:"-"`
}

// UploadFileAuditResponse mirrors UploadFileResponse but exposes audit fields
// Cuttlefish needs (storage_path, org_id, updated_at). It is for admin/audit
// consumers only and never reaches browsers via UploadFileResponse.
type UploadFileAuditResponse struct {
	ID          string     `json:"id"`
	JobID       string     `json:"job_id"`
	FileName    string     `json:"file_name"`
	StoragePath string     `json:"storage_path"`
	FileSize    int64      `json:"file_size"`
	ReadType    ReadType   `json:"read_type"`
	Status      FileStatus `json:"status"`
	OrgID       string     `json:"org_id,omitempty"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

// UploadFileListResponse is the list envelope for the file-level audit endpoint.
type UploadFileListResponse struct {
	Total int64                    `json:"total"`
	Items []UploadFileAuditResponse `json:"items"`
}

func (UploadJob) TableName() string {
	return "upload_jobs"
}

func (UploadFile) TableName() string {
	return "upload_files"
}
