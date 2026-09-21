package service

import (
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestCVMTaskReadyForRequestedDelete(t *testing.T) {
	now := time.Now().UTC()
	task := &model.Task{Executor: model.ExecutorCVM, Status: model.TaskStatusFailed, ExecutionPhase: "terminating", DeleteRequestedAt: &now}
	if cvmTaskReadyForRequestedDelete(task) {
		t.Fatal("task must remain visible while its CVM release is pending")
	}
	task.ExecutionPhase = "terminal"
	if !cvmTaskReadyForRequestedDelete(task) {
		t.Fatal("terminal task with a durable delete request should be hidden")
	}
	task.DeleteRequestedAt = nil
	if cvmTaskReadyForRequestedDelete(task) {
		t.Fatal("terminal task without a delete request must remain visible")
	}
}

func TestCVMTerminalEventNeedsReconciliation(t *testing.T) {
	task := &model.Task{LastCVMEventVersion: 58, ExecutionPhase: "terminating"}
	event := model.CVMStateEvent{Version: 58, ExecutionPhase: "terminal"}
	if !cvmTerminalEventNeedsReconciliation(task, event) {
		t.Fatal("same-version terminal event should repair a task still marked terminating")
	}

	task.ExecutionPhase = "terminal"
	if cvmTerminalEventNeedsReconciliation(task, event) {
		t.Fatal("a task already at terminal should use the duplicate-event path")
	}

	task.ExecutionPhase = "terminating"
	event.Version++
	if cvmTerminalEventNeedsReconciliation(task, event) {
		t.Fatal("a different event version is not a duplicate reconciliation")
	}
}

func TestTerminalStateClassificationForDeletedTaskReplay(t *testing.T) {
	for _, state := range []string{"TERMINATED", "RECLAIMED", "LAUNCH_FAILED", " reclaimed "} {
		if !cvmAttemptStateTerminal(state) {
			t.Fatalf("expected %q to be terminal", state)
		}
	}
	for _, state := range []string{"", "TERMINATING", "RUNNING", "RELEASE_FAILED"} {
		if cvmAttemptStateTerminal(state) {
			t.Fatalf("did not expect %q to be terminal", state)
		}
	}
}
