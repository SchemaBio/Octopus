package model

import "time"

// ResultPackageStatus is the lifecycle of a cached task result archive.
type ResultPackageStatus string

const (
	ResultPackagePending  ResultPackageStatus = "pending"
	ResultPackageBuilding ResultPackageStatus = "building"
	ResultPackageReady    ResultPackageStatus = "ready"
	ResultPackageFailed   ResultPackageStatus = "failed"
)

// ResultPackage stores the immutable source fingerprint and cached ZIP object
// for one execution attempt. The object itself remains in COS/S3.
type ResultPackage struct {
	ID                 string              `json:"id" gorm:"primaryKey;size:36"`
	TaskUUID           string              `json:"task_uuid" gorm:"size:36;uniqueIndex:idx_result_package_attempt,priority:1"`
	ExecutionAttemptID string              `json:"execution_attempt_id" gorm:"size:36;uniqueIndex:idx_result_package_attempt,priority:2"`
	OwnerUserID        uint                `json:"owner_user_id" gorm:"index"`
	ExternalOrgID      string              `json:"external_org_id,omitempty" gorm:"size:100;index"`
	ObjectKey          string              `json:"-" gorm:"size:1000"`
	SourceFingerprint  string              `json:"source_fingerprint,omitempty" gorm:"size:64;index"`
	Status             ResultPackageStatus `json:"status" gorm:"size:20;index"`
	SizeBytes          int64               `json:"size_bytes"`
	Error              string              `json:"error,omitempty" gorm:"type:text"`
	StartedAt          *time.Time          `json:"started_at,omitempty" gorm:"type:timestamptz"`
	FinishedAt         *time.Time          `json:"finished_at,omitempty" gorm:"type:timestamptz"`
	CreatedAt          time.Time           `json:"created_at" gorm:"type:timestamptz"`
	UpdatedAt          time.Time           `json:"updated_at" gorm:"type:timestamptz"`
}

func (ResultPackage) TableName() string { return "result_packages" }

// ResultPackageResponse is safe to return to the browser. The URL is short
// lived and is generated only for ready packages.
type ResultPackageResponse struct {
	TaskUUID           string              `json:"task_uuid"`
	ExecutionAttemptID string              `json:"execution_attempt_id"`
	Status             ResultPackageStatus `json:"status"`
	ResultPackageURL   string              `json:"result_package_url,omitempty"`
	FileName           string              `json:"result_package_filename,omitempty"`
	SizeBytes          int64               `json:"result_package_size_bytes,omitempty"`
	ExpiresAt          *time.Time          `json:"result_package_expires_at,omitempty"`
	SourceFingerprint  string              `json:"source_fingerprint,omitempty"`
	Error              string              `json:"error,omitempty"`
	StartedAt          *time.Time          `json:"started_at,omitempty"`
	FinishedAt         *time.Time          `json:"finished_at,omitempty"`
}
