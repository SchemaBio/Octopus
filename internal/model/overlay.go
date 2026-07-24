package model

import "time"

const (
	OverlayAdmissionActionCreate = "create"
	OverlayAdmissionActionStart  = "start"
	OverlayAdmissionActionRetry  = "retry"
)

const (
	OverlayTaskEventCreated     = "task.created"
	OverlayTaskEventRunning     = "task.running"
	OverlayTaskEventQueued      = "task.queued"
	OverlayTaskEventCompleted   = "task.completed"
	OverlayTaskEventFailed      = "task.failed"
	OverlayTaskEventCancelled   = "task.cancelled"
	OverlayTaskEventStartFailed = "task.start_failed"
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

type OverlayCreditChargeRequest struct {
	Actor       OverlayActor `json:"actor"`
	OrgID       string       `json:"org_id,omitempty"`
	ReferenceID string       `json:"reference_id"`
	Credits     int          `json:"credits"`
	Description string       `json:"description,omitempty"`
}

type OverlayCreditRefundRequest struct {
	Actor       OverlayActor `json:"actor"`
	OrgID       string       `json:"org_id,omitempty"`
	ReferenceID string       `json:"reference_id"`
}

type OverlayCreditResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
	Balance int    `json:"balance,omitempty"`
}
