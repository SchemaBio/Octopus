package model

import "time"

// Sepiida Workflow status
type SepiidaStatus string

const (
	SepiidaStatusUnknown   SepiidaStatus = "unknown"
	SepiidaStatusReady     SepiidaStatus = "ready"
	SepiidaStatusRunning   SepiidaStatus = "running"
	SepiidaStatusSuccess   SepiidaStatus = "success"
	SepiidaStatusFailed    SepiidaStatus = "failed"
	SepiidaStatusCancelled SepiidaStatus = "cancelled"
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
	// Archive metadata fields
	Archived           bool       `json:"archived,omitempty"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
	ArchiveBase        string     `json:"archive_base,omitempty"`
	BasePath           string     `json:"base_path,omitempty"`
	ArchivePrefix      string     `json:"archive_prefix,omitempty"`
	ObjectPrefix       string     `json:"object_prefix,omitempty"`
	KeyPrefix          string     `json:"key_prefix,omitempty"`
	OutputsResolvedKey string     `json:"outputs_resolved_key,omitempty"`
	ArchivedCount      int        `json:"archived_count,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// SepiidaTask represents task status from Sepiida
type SepiidaTask struct {
	ID         string        `json:"id"`
	WorkflowID string        `json:"workflow_id"`
	UUID       string        `json:"uuid"`
	Name       string        `json:"name"`
	JobName    string        `json:"job_name"`
	Status     SepiidaStatus `json:"status"`
	StartTime  *time.Time    `json:"start_time,omitempty"`
	EndTime    *time.Time    `json:"end_time,omitempty"`
	Retries    int           `json:"retries"`
	Runtime    int           `json:"runtime"` // seconds
	CPU        int           `json:"cpu"`
	Memory     int           `json:"memory"` // MB
	Stdout     string        `json:"stdout,omitempty"`
	Stderr     string        `json:"stderr,omitempty"`
	OutputDir  string        `json:"output_dir"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
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

// SepiidaArchiveMetadata extracts archive-related fields from a workflow.
type SepiidaArchiveMetadata struct {
	Archived           bool
	ArchivedAt         *time.Time
	ArchiveBase        string
	ArchivePrefix      string
	OutputsResolvedKey string
	ArchivedCount      int
}

// NormalizedArchiveMetadata extracts and normalizes archive metadata from the workflow,
// providing backward compatibility across Sepiida releases.
func (w *SepiidaWorkflow) NormalizedArchiveMetadata() SepiidaArchiveMetadata {
	return SepiidaArchiveMetadata{
		Archived:           w.Archived,
		ArchivedAt:         w.ArchivedAt,
		ArchiveBase:        firstNonEmptyString(w.ArchiveBase, w.BasePath),
		ArchivePrefix:      firstNonEmptyString(w.ArchivePrefix, w.ObjectPrefix, w.KeyPrefix),
		OutputsResolvedKey: w.OutputsResolvedKey,
		ArchivedCount:      w.ArchivedCount,
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
