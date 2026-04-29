package model

import (
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// ExecutorType represents the execution environment
type ExecutorType string

const (
	ExecutorLocal ExecutorType = "local"  // miniwdl + local.cfg
	ExecutorSlurm ExecutorType = "slurm"  // miniwdl-slurm + slurm.cfg
	ExecutorLSF   ExecutorType = "lsf"    // miniwdl-lsf + lsf.cfg
)

// Task represents a workflow task
type Task struct {
	ID          string       `json:"id" gorm:"primaryKey"`
	UUID        string       `json:"uuid" gorm:"uniqueIndex"`        // Workflow UUID (standard format for Sepiida)
	Name        string       `json:"name"`
	Template    string       `json:"template"`    // WDL template name
	Executor    ExecutorType `json:"executor"`    // Execution environment: local/slurm/lsf
	InputJSON   string       `json:"input_json"`  // Input parameters JSON
	ConfigFile  string       `json:"config_file"` // Config file path
	OutputDir   string       `json:"output_dir"`  // Output directory
	Status      TaskStatus   `json:"status" gorm:"index"`
	PID         int          `json:"pid"`         // Process ID
	SampleID    uint         `json:"sample_id" gorm:"index"`        // Associated sample ID
	ProjectID   uint         `json:"project_id" gorm:"index"`      // Associated project ID
	CreatedBy   uint         `json:"created_by" gorm:"index"`      // User ID who created this task
	StartedAt   *time.Time   `json:"started_at"`
	FinishedAt  *time.Time   `json:"finished_at"`
	CreatedAt   time.Time    `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time    `json:"updated_at" gorm:"autoUpdateTime"`
	Error       string       `json:"error,omitempty"`
	LogPath     string       `json:"log_path,omitempty"`
}

// TaskCreateRequest is the request body for creating a task
type TaskCreateRequest struct {
	Name       string                 `json:"name" binding:"required"`
	Template   string                 `json:"template" binding:"required"`
	Executor   ExecutorType           `json:"executor"`    // Optional, default from config
	Inputs     map[string]interface{} `json:"inputs" binding:"required"`
	ConfigFile string                 `json:"config_file"` // Optional, auto-select based on executor
	OutputDir  string                 `json:"output_dir"`  // Optional, auto-generate if empty
	SampleID   uint                   `json:"sample_id"`   // Optional, associate with sample
	ProjectID  uint                   `json:"project_id"`  // Optional, associate with project
}

// TaskListQuery is the query parameters for listing tasks
type TaskListQuery struct {
	Status     TaskStatus   `form:"status"`
	Executor   ExecutorType `form:"executor"`
	SampleID   uint         `form:"sample_id"`
	ProjectID  uint         `form:"project_id"`
	Page       int          `form:"page" binding:"min=1"`
	PageSize   int          `form:"page_size" binding:"min=1,max=100"`
}

// TaskResponse is the response for a single task
type TaskResponse struct {
	ID         string                 `json:"id"`
	UUID       string                 `json:"uuid"`       // Workflow UUID
	Name       string                 `json:"name"`
	Template   string                 `json:"template"`
	Executor   ExecutorType           `json:"executor"`   // Execution environment
	Inputs     map[string]interface{} `json:"inputs,omitempty"`
	Status     TaskStatus             `json:"status"`
	SampleID   uint                   `json:"sample_id,omitempty"`
	ProjectID  uint                   `json:"project_id,omitempty"`
	CreatedBy  uint                   `json:"created_by,omitempty"`
	PID        int                    `json:"pid,omitempty"`
	StartedAt  *time.Time             `json:"started_at,omitempty"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	Error      string                 `json:"error,omitempty"`
	LogPath    string                 `json:"log_path,omitempty"`
}

// TaskListResponse is the response for listing tasks
type TaskListResponse struct {
	Total int            `json:"total"`
	Items []TaskResponse `json:"items"`
}

// Template represents a WDL template
type Template struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Description string   `json:"description"`
	InputFields []string `json:"input_fields,omitempty"`
}