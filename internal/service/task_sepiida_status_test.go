package service

import (
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestDelayedRunningReportPreservesCVMArchivePhase(t *testing.T) {
	now := time.Now().UTC()
	task := &model.Task{Executor: model.ExecutorCVM, Status: model.TaskStatusRunning, ExecutionPhase: "archiving", Progress: 99, PhaseUpdatedAt: &now}
	changed, completed := applySepiidaWorkflowStatus(task, model.SepiidaStatusRunning)
	if changed || completed || task.ExecutionPhase != "archiving" || task.Progress != 99 || task.PhaseUpdatedAt != &now {
		t.Fatalf("stale running report changed archive phase: %+v", task)
	}
}

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

func TestSepiidaClosesLocalExecutionPhase(t *testing.T) {
	for _, executor := range []model.ExecutorType{model.ExecutorLocal, model.ExecutorSlurm, model.ExecutorLSF} {
		for _, scenario := range []struct {
			report model.SepiidaStatus
			status model.TaskStatus
		}{
			{model.SepiidaStatusFailed, model.TaskStatusFailed},
			{model.SepiidaStatusCancelled, model.TaskStatusCancelled},
			{model.SepiidaStatusSuccess, model.TaskStatusCompleted},
		} {
			for _, priorStatus := range []model.TaskStatus{model.TaskStatusRunning, scenario.status} {
				task := &model.Task{Executor: executor, Status: priorStatus, ExecutionPhase: "running"}
				changed, _ := applySepiidaWorkflowStatus(task, scenario.report)
				if !changed || task.Status != scenario.status || task.ExecutionPhase != "terminal" || task.PhaseUpdatedAt == nil {
					t.Fatalf("executor=%s report=%s prior=%s left stale state: %+v", executor, scenario.report, priorStatus, task)
				}
				changed, _ = applySepiidaWorkflowStatus(task, scenario.report)
				if changed {
					t.Fatal("unchanged terminal report must be idempotent")
				}
			}
		}
	}
}
