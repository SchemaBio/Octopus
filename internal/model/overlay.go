package model

import (
	"encoding/json"
	"time"
)

const (
	OverlayAdmissionActionCreate = "create"
	OverlayAdmissionActionStart  = "start"
	OverlayAdmissionActionRetry  = "retry"
)

const (
	OverlayTaskEventCreated   = "task.created"
	OverlayTaskEventRunning   = "task.running"
	OverlayTaskEventQueued    = "task.queued"
	OverlayTaskEventCompleted = "task.completed"
	// OverlayTaskEventArchiveCompleted is emitted only after Octopus has
	// validated and staged the COS archive for the current CVM attempt.
	OverlayTaskEventArchiveCompleted = "task.archive_completed"
	OverlayTaskEventFailed           = "task.failed"
	OverlayTaskEventCancelled        = "task.cancelled"
	OverlayTaskEventStartFailed      = "task.start_failed"
)

// OverlayActor carries the authenticated principal forwarded by an overlay such
// as Squid. Community deployments normally populate this from the local JWT.
type OverlayActor struct {
	UserID uint   `json:"user_id,omitempty"`
	Email  string `json:"email,omitempty"`
	Role   string `json:"role,omitempty"`
	OrgID  string `json:"org_id,omitempty"`
}

// OverlayTaskSnapshot is the stable task shape sent to external overlay policy
// and event services. It intentionally omits local filesystem paths and inputs.
type OverlayTaskSnapshot struct {
	ID               string     `json:"id"`
	UUID             string     `json:"uuid"`
	Name             string     `json:"name"`
	SampleID         string     `json:"sample_id,omitempty"`
	InternalID       string     `json:"internal_id,omitempty"`
	Pipeline         string     `json:"pipeline,omitempty"`
	PipelineVersion  string     `json:"pipeline_version,omitempty"`
	Template         string     `json:"template,omitempty"`
	Executor         string     `json:"executor,omitempty"`
	Status           TaskStatus `json:"status"`
	Progress         int        `json:"progress"`
	EstimatedMinutes int        `json:"estimated_minutes,omitempty"`
	OrgID            string     `json:"org_id,omitempty"`
	CreatedBy        uint       `json:"created_by,omitempty"`
	Error            string     `json:"error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	AttemptID        string     `json:"attempt_id,omitempty"`
}

// NewOverlayTaskSnapshot converts a task to the overlay contract.
func NewOverlayTaskSnapshot(task *Task) OverlayTaskSnapshot {
	if task == nil {
		return OverlayTaskSnapshot{}
	}
	return OverlayTaskSnapshot{
		ID:               task.ID,
		UUID:             task.UUID,
		Name:             task.Name,
		SampleID:         task.SampleID,
		InternalID:       task.InternalID,
		Pipeline:         task.Pipeline,
		PipelineVersion:  task.PipelineVersion,
		Template:         task.Template,
		Executor:         string(task.Executor),
		Status:           task.Status,
		Progress:         task.Progress,
		EstimatedMinutes: task.EstimatedMinutes,
		OrgID:            task.ExternalOrgID,
		CreatedBy:        task.CreatedBy,
		Error:            task.Error,
		CreatedAt:        task.CreatedAt,
		StartedAt:        task.StartedAt,
		FinishedAt:       task.FinishedAt,
		AttemptID:        task.ExecutionAttemptID,
	}
}

// OverlayTaskAdmissionRequest asks an external policy plane whether a task
// operation may proceed.
type OverlayTaskAdmissionRequest struct {
	Action      string              `json:"action"`
	Actor       OverlayActor        `json:"actor"`
	Task        OverlayTaskSnapshot `json:"task"`
	RequestedAt time.Time           `json:"requested_at"`
}

// OverlayTaskAdmissionResponse is returned by the external policy plane.
type OverlayTaskAdmissionResponse struct {
	Allowed  bool                   `json:"allowed"`
	Reason   string                 `json:"reason,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// OverlayTaskEventRequest notifies an external control plane about task state
// changes. Delivery is best effort and must not be required for community mode.
type OverlayTaskEventRequest struct {
	Event          string              `json:"event"`
	Actor          OverlayActor        `json:"actor"`
	Task           OverlayTaskSnapshot `json:"task"`
	PreviousStatus TaskStatus          `json:"previous_status,omitempty"`
	OccurredAt     time.Time           `json:"occurred_at"`
	Message        string              `json:"message,omitempty"`
}

type CVMInputDownload struct {
	ObjectKey string `json:"object_key"`
	URL       string `json:"url"`
	Target    string `json:"target"`
}

type CVMInlineFile struct {
	Target  string `json:"target"`
	Content string `json:"content"`
}

type CVMExecutionSpec struct {
	Template                string             `json:"template"`
	ReferenceGenome         string             `json:"reference_genome"`
	WorkflowContractVersion string             `json:"workflow_contract_version"`
	Inputs                  json.RawMessage    `json:"inputs"`
	Downloads               []CVMInputDownload `json:"downloads,omitempty"`
	InlineFiles             []CVMInlineFile    `json:"inline_files,omitempty"`
}

type CVMDispatchRequest struct {
	Actor       OverlayActor        `json:"actor"`
	Task        OverlayTaskSnapshot `json:"task"`
	AttemptID   string              `json:"attempt_id"`
	Execution   CVMExecutionSpec    `json:"execution"`
	RequestedAt time.Time           `json:"requested_at"`
}

// CVMInputRefreshRequest/Response are internal control-plane messages used
// by Squid before a queued spot retry. They are authenticated by the same
// service secret as CVM lifecycle callbacks and never reach the browser.
type CVMInputRefreshRequest struct {
	TaskUUID  string `json:"task_uuid"`
	AttemptID string `json:"attempt_id"`
}

type CVMInputRefreshResponse struct {
	TaskUUID  string           `json:"task_uuid"`
	AttemptID string           `json:"attempt_id"`
	Execution CVMExecutionSpec `json:"execution"`
}

type CVMDispatchResponse struct {
	Accepted        bool       `json:"accepted"`
	AttemptID       string     `json:"attempt_id"`
	InstanceID      string     `json:"instance_id,omitempty"`
	InstanceState   string     `json:"instance_state,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	RetryAt         *time.Time `json:"retry_at,omitempty"`
	RetryDeadlineAt *time.Time `json:"retry_deadline_at,omitempty"`
	RetryCount      int        `json:"retry_count,omitempty"`
}

type CVMCancelRequest struct {
	Actor     OverlayActor `json:"actor"`
	TaskUUID  string       `json:"task_uuid"`
	AttemptID string       `json:"attempt_id,omitempty"`
	Reason    string       `json:"reason,omitempty"`
}

type CVMStateEvent struct {
	TaskUUID      string     `json:"task_uuid"`
	AttemptID     string     `json:"attempt_id"`
	InstanceID    string     `json:"instance_id,omitempty"`
	InstanceState string     `json:"instance_state"`
	TaskStatus    TaskStatus `json:"task_status,omitempty"`
	Message       string     `json:"message,omitempty"`
	// Squid includes the durable attempt timestamps so a cancellation that
	// reconciles an instance after an unknown dispatch can be billed by actual
	// runtime even if Octopus never observed a separate running event.
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	RetryAt         *time.Time `json:"retry_at,omitempty"`
	RetryDeadlineAt *time.Time `json:"retry_deadline_at,omitempty"`
	RetryCount      int        `json:"retry_count,omitempty"`
	OccurredAt      time.Time  `json:"occurred_at"`
}

type OverlayCreditChargeRequest struct {
	Actor       OverlayActor `json:"actor"`
	OrgID       string       `json:"org_id,omitempty"`
	ReferenceID string       `json:"reference_id"`
	Credits     int          `json:"credits,omitempty"`
	BillingCode string       `json:"billing_code,omitempty"`
	Quantity    int          `json:"quantity,omitempty"`
	Description string       `json:"description,omitempty"`
}

type OverlayCreditRefundRequest struct {
	Actor       OverlayActor `json:"actor"`
	OrgID       string       `json:"org_id,omitempty"`
	ReferenceID string       `json:"reference_id"`
}

type OverlayCreditResponse struct {
	Allowed        bool   `json:"allowed"`
	Reason         string `json:"reason,omitempty"`
	Balance        int    `json:"balance,omitempty"`
	CreditsCharged int    `json:"credits_charged,omitempty"`
}

const BillingCodeCNVBaselineInput = "cnv_baseline_input_gib"
