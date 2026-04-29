package model

import "time"

// SampleStatus represents the status of a sample
type SampleStatus string

const (
	SampleStatusPending    SampleStatus = "pending"     // 待处理
	SampleStatusProcessing SampleStatus = "processing"  // 处理中
	SampleStatusCompleted  SampleStatus = "completed"   // 已完成
	SampleStatusFailed     SampleStatus = "failed"      // 处理失败
)

// SampleType represents the type of a sample
type SampleType string

const (
	SampleTypeBlood   SampleType = "blood"    // 血液
	SampleTypeTissue  SampleType = "tissue"   // 组织
	SampleTypeSaliva  SampleType = "saliva"   // 唾液
	SampleTypeBuccal  SampleType = "buccal"   // 口腔拭子
	SampleTypeOther   SampleType = "other"    // 其他
)

// Sample represents a biological sample
type Sample struct {
	ID          uint         `json:"id" gorm:"primaryKey"`
	SampleID    string       `json:"sample_id" gorm:"uniqueIndex;size:50;not null"`      // 样本编号
	SampleName  string       `json:"sample_name" gorm:"size:100"`                         // 样本名称
	SampleType  SampleType   `json:"sample_type" gorm:"size:20;default:other"`            // 样本类型
	Source      string       `json:"source" gorm:"size:100"`                              // 样本来源
	ProjectID   uint         `json:"project_id" gorm:"index"`                             // 所属项目ID
	Status      SampleStatus `json:"status" gorm:"size:20;default:pending"`               // 样本状态
	Metadata    string       `json:"metadata" gorm:"type:text"`                           // JSON格式的元数据
	CreatedBy   uint         `json:"created_by" gorm:"index"`                             // 创建人ID
	CreatedAt   time.Time    `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time    `json:"updated_at" gorm:"autoUpdateTime"`
}

// SampleCreateRequest is the request body for creating a sample
type SampleCreateRequest struct {
	SampleID   string     `json:"sample_id" binding:"required"`
	SampleName string     `json:"sample_name"`
	SampleType SampleType `json:"sample_type"`
	Source     string     `json:"source"`
	ProjectID  uint       `json:"project_id"`  // Optional, can assign later
	Metadata   string     `json:"metadata"`    // JSON string
}

// SampleUpdateRequest is the request body for updating a sample
type SampleUpdateRequest struct {
	SampleName string       `json:"sample_name"`
	SampleType SampleType   `json:"sample_type"`
	Source     string       `json:"source"`
	ProjectID  uint         `json:"project_id"`
	Status     SampleStatus `json:"status"`
	Metadata   string       `json:"metadata"`
}

// SampleListQuery is the query parameters for listing samples
type SampleListQuery struct {
	ProjectID uint         `form:"project_id"`
	Status    SampleStatus `form:"status"`
	SampleType SampleType  `form:"sample_type"`
	Page      int          `form:"page" binding:"min=1"`
	PageSize  int          `form:"page_size" binding:"min=1,max=100"`
}

// SampleResponse is the response for a single sample
type SampleResponse struct {
	ID          uint         `json:"id"`
	SampleID    string       `json:"sample_id"`
	SampleName  string       `json:"sample_name"`
	SampleType  SampleType   `json:"sample_type"`
	Source      string       `json:"source"`
	ProjectID   uint         `json:"project_id"`
	Status      SampleStatus `json:"status"`
	Metadata    string       `json:"metadata,omitempty"`
	CreatedBy   uint         `json:"created_by"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// SampleListResponse is the response for listing samples
type SampleListResponse struct {
	Total int              `json:"total"`
	Items []SampleResponse `json:"items"`
}

// TableName specifies the table name for Sample
func (Sample) TableName() string {
	return "samples"
}