package model

import "time"

// Sepiida Workflow status
type SepiidaStatus string

const (
	SepiidaStatusUnknown    SepiidaStatus = "Unknown"
	SepiidaStatusReady      SepiidaStatus = "Ready"
	SepiidaStatusRunning    SepiidaStatus = "Running"
	SepiidaStatusSuccess    SepiidaStatus = "Success"
	SepiidaStatusFailed     SepiidaStatus = "Failed"
	SepiidaStatusCancelled  SepiidaStatus = "Cancelled"
)

// SepiidaWorkflow represents workflow status from Sepiida
type SepiidaWorkflow struct {
	ID          string        `json:"id"`
	UUID        string        `json:"uuid"`
	Name        string        `json:"name"`
	Status      SepiidaStatus `json:"status"`
	StartTime   *time.Time    `json:"start_time,omitempty"`
	EndTime     *time.Time    `json:"end_time,omitempty"`
	OutputDir   string        `json:"output_dir"`
	OutputsJSON string        `json:"outputs_json,omitempty"`
	AgentID     string        `json:"agent_id,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// SepiidaTask represents task status from Sepiida
type SepiidaTask struct {
	ID           string        `json:"id"`
	WorkflowID   string        `json:"workflow_id"`
	UUID         string        `json:"uuid"`
	Name         string        `json:"name"`
	JobName      string        `json:"job_name"`
	Status       SepiidaStatus `json:"status"`
	StartTime    *time.Time    `json:"start_time,omitempty"`
	EndTime      *time.Time    `json:"end_time,omitempty"`
	Retries      int           `json:"retries"`
	Runtime      int           `json:"runtime"` // seconds
	CPU          int           `json:"cpu"`
	Memory       int           `json:"memory"` // MB
	Stdout       string        `json:"stdout,omitempty"`
	Stderr       string        `json:"stderr,omitempty"`
	OutputDir    string        `json:"output_dir"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// SepiidaWorkflowResponse is the response from Sepiida API
type SepiidaWorkflowResponse struct {
	Workflow SepiidaWorkflow `json:"workflow,omitempty"`
	Tasks    []SepiidaTask   `json:"tasks,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// SepiidaWorkflowsResponse is the response for listing workflows
type SepiidaWorkflowsResponse struct {
	Workflows []SepiidaWorkflow `json:"workflows"`
	Total     int               `json:"total"`
}

// SepiidaTasksResponse is the response for listing tasks
type SepiidaTasksResponse struct {
	Tasks []SepiidaTask `json:"tasks"`
	Total int           `json:"total"`
}

// TaskProgressResponse combines Octopus task with Sepiida progress
type TaskProgressResponse struct {
	ID         string           `json:"id"`
	UUID       string           `json:"uuid"`       // Workflow UUID
	Name       string           `json:"name"`
	Template   string           `json:"template"`
	Status     TaskStatus       `json:"status"`     // Octopus status
	Sepiida    *SepiidaWorkflow `json:"sepiida,omitempty"` // Sepiida status
	Tasks      []SepiidaTask    `json:"tasks,omitempty"`   // Workflow tasks
	CreatedAt  time.Time        `json:"created_at"`
}