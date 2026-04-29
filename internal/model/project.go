package model

import "time"

// ProjectStatus represents the status of a project
type ProjectStatus string

const (
	ProjectStatusActive    ProjectStatus = "active"     // 进行中
	ProjectStatusCompleted ProjectStatus = "completed"  // 已完成
	ProjectStatusArchived  ProjectStatus = "archived"   // 已归档
	ProjectStatusCancelled ProjectStatus = "cancelled"  // 已取消
)

// Project represents a batch/project containing multiple samples
type Project struct {
	ID          uint          `json:"id" gorm:"primaryKey"`
	ProjectCode string        `json:"project_code" gorm:"uniqueIndex;size:50;not null"`  // 项目编号
	ProjectName string        `json:"project_name" gorm:"size:100;not null"`             // 项目名称
	Description string        `json:"description" gorm:"type:text"`                      // 项目描述
	Panel       string        `json:"panel" gorm:"size:50"`                              // 分析 Panel (e.g., germline-wes, somatic-panel)
	Status      ProjectStatus `json:"status" gorm:"size:20;default:active"`              // 项目状态
	CreatedBy   uint          `json:"created_by" gorm:"index"`                           // 创建人ID
	CreatedAt   time.Time     `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time     `json:"updated_at" gorm:"autoUpdateTime"`
}

// ProjectCreateRequest is the request body for creating a project
type ProjectCreateRequest struct {
	ProjectCode string `json:"project_code" binding:"required"`
	ProjectName string `json:"project_name" binding:"required"`
	Description string `json:"description"`
	Panel       string `json:"panel"`  // Analysis panel type
}

// ProjectUpdateRequest is the request body for updating a project
type ProjectUpdateRequest struct {
	ProjectName string        `json:"project_name"`
	Description string        `json:"description"`
	Panel       string        `json:"panel"`
	Status      ProjectStatus `json:"status"`
}

// ProjectListQuery is the query parameters for listing projects
type ProjectListQuery struct {
	Status    ProjectStatus `form:"status"`
	Panel     string        `form:"panel"`
	Page      int           `form:"page" binding:"min=1"`
	PageSize  int           `form:"page_size" binding:"min=1,max=100"`
}

// ProjectResponse is the response for a single project
type ProjectResponse struct {
	ID          uint          `json:"id"`
	ProjectCode string        `json:"project_code"`
	ProjectName string        `json:"project_name"`
	Description string        `json:"description"`
	Panel       string        `json:"panel"`
	Status      ProjectStatus `json:"status"`
	CreatedBy   uint          `json:"created_by"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// ProjectListResponse is the response for listing projects
type ProjectListResponse struct {
	Total int               `json:"total"`
	Items []ProjectResponse `json:"items"`
}

// ProjectSummaryResponse is the response for project summary
type ProjectSummaryResponse struct {
	Project          ProjectResponse `json:"project"`
	TotalSamples     int             `json:"total_samples"`
	CompletedSamples int             `json:"completed_samples"`
	ProcessingSamples int            `json:"processing_samples"`
	PendingSamples   int             `json:"pending_samples"`
	FailedSamples    int             `json:"failed_samples"`
	TotalTasks       int             `json:"total_tasks"`
	CompletedTasks   int             `json:"completed_tasks"`
	RunningTasks     int             `json:"running_tasks"`
	FailedTasks      int             `json:"failed_tasks"`
}

// TableName specifies the table name for Project
func (Project) TableName() string {
	return "projects"
}