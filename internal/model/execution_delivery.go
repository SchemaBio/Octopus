package model

import (
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// CVMSubmission is the durable intent to execute one task attempt.
type CVMSubmission struct {
	RequestJSON string    `gorm:"type:text;default:''"`
	AttemptID   string    `gorm:"primaryKey;size:36"`
	TaskUUID    string    `gorm:"index;size:36"`
	Delivered   bool      `gorm:"default:false;index"`
	NextRetryAt time.Time `gorm:"index"`
	CreatedAt   time.Time
	LastError   string
}

// CVMCancellation is the durable control-plane intent to cancel one execution
// attempt. It is kept separate from the dispatch submission because a cancel
// may be requested before the dispatch has reached Squid, or while the cloud
// outcome of that dispatch is still unknown.
type CVMCancellation struct {
	AttemptID   string    `gorm:"primaryKey;size:36"`
	TaskUUID    string    `gorm:"index;size:36"`
	Payload     string    `gorm:"type:text"`
	Delivered   bool      `gorm:"default:false;index"`
	NextRetryAt time.Time `gorm:"index"`
	CreatedAt   time.Time
	LastError   string
}

type ExecutionOutbox struct {
	ID          string    `gorm:"primaryKey;size:160"`
	Payload     string    `gorm:"type:text"`
	NextRetryAt time.Time `gorm:"index"`
	Delivered   bool      `gorm:"default:false;index"`
	CreatedAt   time.Time
	LastError   string
}

// ExecutionEventID returns the stable id used by both the transactional
// outbox and an optional synchronous delivery. Archive completion is a
// one-time milestone for an attempt, so its id is independent of later task
// versions; this prevents every post-archive save from creating another
// cleanup event.
func ExecutionEventID(attemptID string, version uint64, kind string) string {
	if kind == OverlayTaskEventArchiveCompleted {
		return fmt.Sprintf("%s:archive_completed", attemptID)
	}
	return fmt.Sprintf("%s:%d:%s", attemptID, version, kind)
}

// executionOutboxEvents returns the lifecycle events which must be committed
// together with a task snapshot. Archive completion is deliberately gated on
// the business terminal state: staging a COS archive is an intermediate step
// and must not tell Squid that the workflow is complete while Octopus still
// reports it as running/archiving.
func executionOutboxEvents(t *Task) []string {
	if t == nil {
		return nil
	}
	if t.Executor != ExecutorCVM || t.ExternalOrgID == "" || t.ExecutionAttemptID == "" {
		return nil
	}
	event := ""
	switch t.Status {
	case TaskStatusRunning:
		event = OverlayTaskEventRunning
	case TaskStatusCompleted:
		event = OverlayTaskEventCompleted
	case TaskStatusPendingInterpretation:
		// A CVM can finish computation before a user opens the interpretation
		// view. It is still a terminal execution for resource cleanup and
		// billing, so use the same completion event while preserving the
		// business status in Octopus.
		event = OverlayTaskEventCompleted
	case TaskStatusFailed:
		event = OverlayTaskEventFailed
	case TaskStatusCancelled:
		event = OverlayTaskEventCancelled
	case TaskStatusQueued:
		event = OverlayTaskEventQueued
	}
	if t.ExecutionPhase == "terminating" {
		// Resource termination is separate from the business terminal state.
		// Preserve completed/failed semantics while the VM is being released;
		// otherwise a callback racing the cancellation outbox could turn a
		// successful or failed attempt into a user cancellation and apply the
		// wrong billing action.
		switch t.Status {
		case TaskStatusCompleted, TaskStatusPendingInterpretation:
			event = OverlayTaskEventCompleted
		case TaskStatusFailed:
			event = OverlayTaskEventFailed
		default:
			event = OverlayTaskEventCancelled
		}
	}
	events := make([]string, 0, 2)
	if event != "" {
		events = append(events, event)
	}
	if t.CVMArchiveStagedAt != nil && (t.Status == TaskStatusCompleted || t.Status == TaskStatusPendingInterpretation) {
		events = append(events, OverlayTaskEventArchiveCompleted)
	}
	return events
}

// ExecutionOutboxEvents exposes the deterministic lifecycle event selection to
// upgrade recovery. Recovery can recreate a missing outbox row without saving
// the task again (and therefore without inventing a new task version).
func ExecutionOutboxEvents(t *Task) []string { return executionOutboxEvents(t) }

// NewExecutionOutbox builds one durable event snapshot for an attempt.
func NewExecutionOutbox(t *Task, kind string) (*ExecutionOutbox, error) {
	if t == nil || kind == "" {
		return nil, fmt.Errorf("task and event kind are required")
	}
	id := ExecutionEventID(t.ExecutionAttemptID, t.Version, kind)
	body, err := json.Marshal(OverlayTaskEventRequest{
		EventID: id, Version: t.Version, Event: kind,
		Task: NewOverlayTaskSnapshot(t), OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &ExecutionOutbox{ID: id, Payload: string(body), NextRetryAt: now}, nil
}

// AfterSave makes lifecycle delivery atomic with a SaaS task update. Community
// tasks have no outbox dependency. Duplicate snapshots have deterministic IDs.
func (t *Task) AfterSave(tx *gorm.DB) error {
	if t == nil {
		return nil
	}
	events := executionOutboxEvents(t)
	for _, kind := range events {
		outbox, err := NewExecutionOutbox(t, kind)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(outbox).Error; err != nil {
			return err
		}
	}
	return nil
}
