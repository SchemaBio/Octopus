package model

import (
	"testing"
	"time"
)

func TestExecutionOutboxEventsDoNotReleaseBeforeBusinessCompletion(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	task := &Task{
		Executor:           ExecutorCVM,
		ExternalOrgID:      "org-1",
		ExecutionAttemptID: "attempt-1",
		Status:             TaskStatusRunning,
		ExecutionPhase:     "archiving",
		CVMArchiveStagedAt: nil,
	}
	if events := executionOutboxEvents(task); len(events) != 1 || events[0] != OverlayTaskEventRunning {
		t.Fatalf("unexpected pre-archive events: %#v", events)
	}
	task.CVMArchiveStagedAt = &now
	if events := executionOutboxEvents(task); len(events) != 1 || events[0] != OverlayTaskEventRunning {
		t.Fatalf("archive staging must not emit completion while running: %#v", events)
	}

	task.Status = TaskStatusCompleted
	task.ExecutionPhase = "archiving"
	events := executionOutboxEvents(task)
	if len(events) != 2 || events[0] != OverlayTaskEventCompleted || events[1] != OverlayTaskEventArchiveCompleted {
		t.Fatalf("completed staged task must emit completion and archive events: %#v", events)
	}
}

func TestTerminatingRunningTaskDoesNotBecomeUserCancellation(t *testing.T) {
	task := &Task{Executor: ExecutorCVM, ExternalOrgID: "org-1", ExecutionAttemptID: "attempt-1", Status: TaskStatusRunning, ExecutionPhase: "terminating"}
	if events := executionOutboxEvents(task); len(events) != 0 {
		t.Fatalf("automatic termination of a running task must not emit a cancellation event: %#v", events)
	}
	task.Status = TaskStatusFailed
	if events := executionOutboxEvents(task); len(events) != 1 || events[0] != OverlayTaskEventFailed {
		t.Fatalf("automatic failure must emit task.failed: %#v", events)
	}
	task.Status = TaskStatusCancelled
	if events := executionOutboxEvents(task); len(events) != 1 || events[0] != OverlayTaskEventCancelled {
		t.Fatalf("explicit cancelled status must emit task.cancelled: %#v", events)
	}
}
