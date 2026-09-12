package service

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestApplySepiidaWorkflowStatusDoesNotFailCVMFromSepiida(t *testing.T) {
	task := &model.Task{
		UUID:           "task-1",
		Executor:       model.ExecutorCVM,
		Status:         model.TaskStatusRunning,
		ExecutionPhase: "running",
	}
	changed, completed := applySepiidaWorkflowStatus(task, model.SepiidaStatusFailed)
	if !changed || completed {
		t.Fatalf("expected a diagnostic update without completion, changed=%v completed=%v", changed, completed)
	}
	if task.Status != model.TaskStatusRunning {
		t.Fatalf("CVM task should stay running, got %s", task.Status)
	}
	if task.ExecutionPhase == "terminating" || task.ExecutionPhase == "terminal" {
		t.Fatalf("CVM task must not enter terminating from Sepiida failed, phase=%s", task.ExecutionPhase)
	}
	if task.FinishedAt != nil {
		t.Fatal("CVM task must not get FinishedAt from Sepiida failed")
	}
}

func TestApplySepiidaWorkflowStatusDoesNotCancelCVMFromSepiida(t *testing.T) {
	task := &model.Task{
		UUID:           "task-1",
		Executor:       model.ExecutorCVM,
		Status:         model.TaskStatusRunning,
		ExecutionPhase: "running",
	}
	changed, completed := applySepiidaWorkflowStatus(task, model.SepiidaStatusCancelled)
	if !changed || completed {
		t.Fatalf("expected a diagnostic update without completion, changed=%v completed=%v", changed, completed)
	}
	if task.Status != model.TaskStatusRunning {
		t.Fatalf("CVM task should stay running, got %s", task.Status)
	}
}

func TestApplySepiidaWorkflowStatusFailsLocalExecutor(t *testing.T) {
	task := &model.Task{
		UUID:     "task-1",
		Executor: model.ExecutorLocal,
		Status:   model.TaskStatusRunning,
	}
	changed, completed := applySepiidaWorkflowStatus(task, model.SepiidaStatusFailed)
	if !changed || completed {
		t.Fatalf("expected local failure, changed=%v completed=%v", changed, completed)
	}
	if task.Status != model.TaskStatusFailed {
		t.Fatalf("local executor should fail from Sepiida, got %s", task.Status)
	}
	if task.FinishedAt == nil {
		t.Fatal("local failure should set FinishedAt")
	}
}

func TestApplySepiidaWorkflowStatusCompletesOnSuccess(t *testing.T) {
	task := &model.Task{
		UUID:           "task-1",
		Executor:       model.ExecutorCVM,
		Status:         model.TaskStatusRunning,
		ExecutionPhase: "archiving",
	}
	changed, completed := applySepiidaWorkflowStatus(task, model.SepiidaStatusSuccess)
	if !changed || !completed {
		t.Fatalf("expected completion, changed=%v completed=%v", changed, completed)
	}
	if task.Status != model.TaskStatusCompleted {
		t.Fatalf("got status %s", task.Status)
	}
}
