package model

import (
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusQueued                TaskStatus = "queued"
	TaskStatusWaitingData           TaskStatus = "waiting_for_data"
	TaskStatusRunning               TaskStatus = "running"
	TaskStatusCompleted             TaskStatus = "completed"
	TaskStatusFailed                TaskStatus = "failed"
	TaskStatusCancelled             TaskStatus = "cancelled"
	TaskStatusPendingInterpretation TaskStatus = "pending_interpretation"
)

// ResultImportStatus tracks structured result import after archive completion.
type ResultImportStatus string

const (
	ResultImportStatusPending ResultImportStatus = "pending"
	ResultImportStatusRunning ResultImportStatus = "running"
	ResultImportStatusSuccess ResultImportStatus = "success"
	ResultImportStatusFailed  ResultImportStatus = "failed"
)

// ExecutorType represents the execution environment
type ExecutorType string

const (
	ExecutorLocal ExecutorType = "local" // miniwdl + local.cfg
	ExecutorSlurm ExecutorType = "slurm" // miniwdl-slurm + slurm.cfg
	ExecutorLSF   ExecutorType = "lsf"   // miniwdl-lsf + lsf.cfg
)

// Task represents a workflow task
type Task struct {
	ID               string       `json:"id" gorm:"primaryKey"`
	UUID             string       `json:"uuid" gorm:"uniqueIndex"` // Workflow UUID (standard format for Sepiida)
	Name             string       `json:"name"`
	SampleID         string       `json:"sample_id" gorm:"size:36;index"`     // Sample UUID
	InternalID       string       `json:"internal_id" gorm:"size:100"`        // Sample internal_id for display
	UploadJobID      string       `json:"upload_job_id" gorm:"size:36;index"` // UploadJob UUID
	Pipeline         string       `json:"pipeline" gorm:"size:200"`           // Pipeline name
	PipelineVersion  string       `json:"pipeline_version" gorm:"size:50"`
	Template         string       `json:"template"`            // WDL template name (internal)
	Executor         ExecutorType `json:"executor"`            // Execution environment (internal)
	InputJSON        string       `json:"-" gorm:"type:jsonb"` // Input parameters JSON (internal)
	ConfigFile       string       `json:"-"`                   // Config file path (internal)
	OutputDir        string       `json:"-"`                   // Output directory (internal)
	Status           TaskStatus   `json:"status" gorm:"size:30;index"`
	Progress         int          `json:"progress" gorm:"type:smallint"` // 0-100
	PID              int          `json:"-"`                             // Process ID (internal)
	Remark           string       `json:"remark,omitempty" gorm:"type:text"`
	SampleIDRef      uint         `json:"-" gorm:"index"`          // FK to samples.id (internal)
	ProjectID        uint         `json:"-" gorm:"index"`          // FK to projects.id (internal)
	ExternalOrgID    string       `json:"-" gorm:"size:100;index"` // optional external tenant reference for overlay events
	EstimatedMinutes int          `json:"-" gorm:"default:0"`      // optional runtime estimate for overlay policy
	CreatedBy        uint         `json:"created_by" gorm:"index"`
	StartedAt        *time.Time   `json:"started_at,omitempty" gorm:"type:timestamptz"`
	FinishedAt       *time.Time   `json:"finished_at,omitempty" gorm:"type:timestamptz"`
	CreatedAt        time.Time    `json:"created_at" gorm:"autoCreateTime;type:timestamptz"`
	UpdatedAt        time.Time    `json:"updated_at" gorm:"autoUpdateTime;type:timestamptz"`
	Error            string       `json:"error,omitempty"`
	LogPath          string       `json:"-"` // Log file path (internal)

	// VMStatus is the lifecycle state of the provisioned VM for this task. It
	// supersedes the legacy cvm_instances.status concept (that table is not owned
	// by any service). Stored as a plain string enum so values can evolve
	// without enum-type migrations; populated by the task runner / overlay events.
	VMStatus string `json:"vm_status,omitempty" gorm:"size:30;index"`

	ResultImportStatus      ResultImportStatus `json:"-" gorm:"size:30;default:'pending'"`
	ResultImportError       string             `json:"-" gorm:"type:text"`
	ResultImportedAt        *time.Time         `json:"-" gorm:"type:timestamptz"`
	ResultImportFingerprint string             `json:"-" gorm:"size:64;index"`
	ResultImportAttempts    int                `json:"-" gorm:"default:0"`
}

// TaskCreateRequest is the request body for creating a task
type TaskCreateRequest struct {
	SampleID         string `json:"sampleId" binding:"required"`
	InternalID       string `json:"internalId"`
	UploadJobID      string `json:"uploadJobId"`
	PipelineID       string `json:"pipelineId"`
	PipelineName     string `json:"pipelineName" binding:"required"`
	PipelineVersion  string `json:"pipelineVersion"`
	Remark           string `json:"remark"`
	EstimatedMinutes int    `json:"estimatedMinutes"`
	// Internal fields (not from frontend, but used internally)
	Template   string                 `json:"template,omitempty"`
	Executor   ExecutorType           `json:"executor,omitempty"`
	Inputs     map[string]interface{} `json:"inputs,omitempty"`
	ConfigFile string                 `json:"config_file,omitempty"`
	OutputDir  string                 `json:"output_dir,omitempty"`
}

// TaskUpdateRequest is the request body for updating a task
type TaskUpdateRequest struct {
	InternalID string `json:"internalId"`
	Pipeline   string `json:"pipeline"`
	Remark     string `json:"remark"`
}

// TaskListQuery is the query parameters for listing tasks
type TaskListQuery struct {
	Status        TaskStatus `form:"status"`
	SampleID      string     `form:"sampleId"`
	Search        string     `form:"search"`        // text across name/internal_id/error
	CreatedSince  *time.Time `form:"created_since"` // tasks created >= since (RFC3339); nil = no filter
	UpdatedSince  *time.Time `form:"updated_since"` // tasks updated >= since (RFC3339); nil = no filter
	Page          int        `form:"page" binding:"omitempty,min=1"`
	PageSize      int        `form:"page_size" binding:"omitempty,min=1,max=100"`
	CreatedBy     uint       `json:"-"`
	ExternalOrgID string     `json:"-"`
	IncludeAll    bool       `json:"-"`
}

// TaskResponse matches frontend AnalysisTask type
type TaskResponse struct {
	ID              string     `json:"id"`
	SampleID        string     `json:"sampleId"`
	InternalID      string     `json:"internalId"`
	Pipeline        string     `json:"pipeline"`
	PipelineVersion string     `json:"pipelineVersion"`
	Status          TaskStatus `json:"status"`
	Progress        int        `json:"progress"`
	CreatedAt       string     `json:"created_at"`
	CreatedBy       string     `json:"createdBy"`
	CompletedAt     string     `json:"completedAt,omitempty"`
	Remark          string     `json:"remark,omitempty"`
}

// TaskDetailResponse matches frontend AnalysisTaskDetail type
type TaskDetailResponse struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	SampleID        string     `json:"sampleId"`
	InternalID      string     `json:"internalId"`
	Pipeline        string     `json:"pipeline"`
	PipelineVersion string     `json:"pipelineVersion"`
	Status          TaskStatus `json:"status"`
	CreatedAt       string     `json:"created_at"`
	CreatedBy       string     `json:"createdBy"`
	CompletedAt     string     `json:"completedAt,omitempty"`
}

// TaskListResponse is the response for listing tasks
type TaskListResponse struct {
	Total int64          `json:"total"`
	Items []TaskResponse `json:"items"`
}

// TaskAuditResponse is the enriched task shape for cross-org audit/monitoring
// consumers (e.g. Cuttlefish). It deliberately does NOT extend TaskResponse —
// the frontend AnalysisTask contract must stay untouched. It exposes fields
// Cuttlefish's audit queries previously fetched via direct SQL on the tasks
// table (name, executor, vm_status, result_import_*, error, started/finished).
type TaskAuditResponse struct {
	ID                   string             `json:"id"`
	Name                 string             `json:"name"`
	SampleID             string             `json:"sampleId"`
	InternalID           string             `json:"internalId"`
	Pipeline             string             `json:"pipeline"`
	PipelineVersion      string             `json:"pipelineVersion"`
	Executor             ExecutorType       `json:"executor"`
	Status               TaskStatus         `json:"status"`
	Progress             int                `json:"progress"`
	VMStatus             string             `json:"vm_status,omitempty"`
	ResultImportStatus   ResultImportStatus `json:"result_import_status,omitempty"`
	ResultImportAttempts int                `json:"result_import_attempts,omitempty"`
	Error                string             `json:"error,omitempty"`
	CreatedAt            string             `json:"created_at"`
	UpdatedAt            string             `json:"updated_at,omitempty"`
	StartedAt            string             `json:"started_at,omitempty"`
	FinishedAt           string             `json:"finished_at,omitempty"`
	CompletedAt          string             `json:"completedAt,omitempty"` // alias of FinishedAt for compat
	CreatedBy            string             `json:"createdBy"`
	OrgID                string             `json:"org_id,omitempty"` // audit consumers need org
	Remark               string             `json:"remark,omitempty"`
}

// ToAuditResponse converts a Task to the enriched audit response shape.
// It reads the result-import fields directly off the struct (the `json:"-"`
// tags on those fields only affect default marshalling, not field access).
func (t *Task) ToAuditResponse() TaskAuditResponse {
	resp := TaskAuditResponse{
		ID:                   t.UUID,
		Name:                 t.Name,
		SampleID:             t.SampleID,
		InternalID:           t.InternalID,
		Pipeline:             t.Pipeline,
		PipelineVersion:      t.PipelineVersion,
		Executor:             t.Executor,
		Status:               t.Status,
		Progress:             t.Progress,
		VMStatus:             t.VMStatus,
		ResultImportStatus:   t.ResultImportStatus,
		ResultImportAttempts: t.ResultImportAttempts,
		Error:                t.Error,
		CreatedAt:            t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:            t.UpdatedAt.Format(time.RFC3339),
		CreatedBy:            formatID(t.CreatedBy),
		OrgID:                t.ExternalOrgID,
		Remark:               t.Remark,
	}
	if t.StartedAt != nil {
		resp.StartedAt = t.StartedAt.Format(time.RFC3339)
	}
	if t.FinishedAt != nil {
		resp.FinishedAt = t.FinishedAt.Format(time.RFC3339)
		resp.CompletedAt = resp.FinishedAt
	}
	return resp
}

// TaskAuditListResponse mirrors TaskListResponse for the audit endpoint.
type TaskAuditListResponse struct {
	Total int64               `json:"total"`
	Items []TaskAuditResponse `json:"items"`
}

// TaskStatsResponse is the cross-org monitoring overview for Cuttlefish. It
// provides the counts Cuttlefish's getOverview/getRiskSignals previously
// computed via direct SQL: totals, running, failed-in-last-24h, status
// distribution, and failed result-import batches in the last 7 days.
type TaskStatsResponse struct {
	TotalTasks               int            `json:"total_tasks"`
	RunningTasks             int            `json:"running_tasks"`
	FailedLast24h            int            `json:"failed_last_24h"`
	StatusDistribution       map[string]int `json:"status_distribution"` // status->count (all time)
	ResultImportFailedLast7d int            `json:"result_import_failed_last_7d"`
	WindowStart              string         `json:"window_start"` // RFC3339 of "now-24h"
}

// TaskProgressResponse is the response for task progress
type TaskProgressResponse struct {
	ID                      string             `json:"id"`
	UUID                    string             `json:"uuid"`
	Name                    string             `json:"name"`
	Template                string             `json:"template"`
	Status                  TaskStatus         `json:"status"`
	Progress                int                `json:"progress"`
	CreatedAt               time.Time          `json:"created_at"`
	ResultImportStatus      ResultImportStatus `json:"result_import_status,omitempty"`
	ResultImportError       string             `json:"result_import_error,omitempty"`
	ResultImportedAt        *time.Time         `json:"result_imported_at,omitempty"`
	ResultImportFingerprint string             `json:"result_import_fingerprint,omitempty"`
	ResultImportAttempts    int                `json:"result_import_attempts,omitempty"`
	Sepiida                 *SepiidaWorkflow   `json:"sepiida,omitempty"`
	Tasks                   []SepiidaTask      `json:"tasks,omitempty"`
}

// Template represents a WDL template
type Template struct {
	Name          string                 `json:"name"`
	ShortName     string                 `json:"shortName,omitempty"`
	Domain        string                 `json:"domain,omitempty"`
	Workflow      string                 `json:"workflow,omitempty"`
	Path          string                 `json:"path,omitempty"`
	Description   string                 `json:"description"`
	InputFields   []string               `json:"input_fields,omitempty"`
	DefaultInputs map[string]interface{} `json:"default_inputs,omitempty"`
}

// ToResponse converts Task to TaskResponse
func (t *Task) ToResponse() TaskResponse {
	resp := TaskResponse{
		ID:              t.UUID,
		SampleID:        t.SampleID,
		InternalID:      t.InternalID,
		Pipeline:        t.Pipeline,
		PipelineVersion: t.PipelineVersion,
		Status:          t.Status,
		Progress:        t.Progress,
		CreatedAt:       t.CreatedAt.Format(time.RFC3339),
		CreatedBy:       formatID(t.CreatedBy),
		Remark:          t.Remark,
	}
	if t.FinishedAt != nil {
		resp.CompletedAt = t.FinishedAt.Format(time.RFC3339)
	}
	return resp
}

// ToDetailResponse converts Task to TaskDetailResponse
func (t *Task) ToDetailResponse() TaskDetailResponse {
	resp := TaskDetailResponse{
		ID:              t.UUID,
		Name:            t.Name,
		SampleID:        t.SampleID,
		InternalID:      t.InternalID,
		Pipeline:        t.Pipeline,
		PipelineVersion: t.PipelineVersion,
		Status:          t.Status,
		CreatedAt:       t.CreatedAt.Format(time.RFC3339),
		CreatedBy:       formatID(t.CreatedBy),
	}
	if t.FinishedAt != nil {
		resp.CompletedAt = t.FinishedAt.Format(time.RFC3339)
	}
	return resp
}

// TableName specifies the table name for Task
func (Task) TableName() string {
	return "tasks"
}
