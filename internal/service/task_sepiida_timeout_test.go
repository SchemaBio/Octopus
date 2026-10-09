package service

import (
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestCVMRetryDoesNotInheritFirstReportDeadline(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-8 * time.Hour)
	task := model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "running",
		SepiidaFirstReportExpectedAt: &old, BootstrapLastHeartbeatAt: &old,
		BootstrapPhase: "running", DiagnosticSummary: "old attempt"}
	if !sepiidaFirstReportOverdue(&task, 10*time.Minute, now) {
		t.Fatal("old attempt should be overdue")
	}
	resetCVMWorkflowObservation(&task)
	if sepiidaFirstReportOverdue(&task, 10*time.Minute, now) {
		t.Fatal("retry inherited the old first-report timeout")
	}
	task.SepiidaFirstReportExpectedAt = &now
	if sepiidaFirstReportOverdue(&task, 10*time.Minute, now.Add(time.Minute)) {
		t.Fatal("new workflow should get its own first-report window")
	}
}

func TestSepiidaFirstReportOverdueUsesWorkflowStartDeadline(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	timeout := 10 * time.Minute
	oldPhaseUpdate := now.Add(-45 * time.Minute)
	justRefreshedPhase := now.Add(-time.Minute)
	startedLongAgo := now.Add(-11 * time.Minute)
	startedRecently := now.Add(-9 * time.Minute)
	reported := now.Add(-time.Minute)

	tests := []struct {
		name string
		task model.Task
		want bool
	}{
		{
			name: "long database bootstrap does not consume the Sepiida deadline",
			task: model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "bootstrapping", PhaseUpdatedAt: &oldPhaseUpdate},
			want: false,
		},
		{
			name: "missing persistent workflow start marker does not time out",
			task: model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "running", PhaseUpdatedAt: &oldPhaseUpdate},
			want: false,
		},
		{
			name: "running deadline is not extended by recent phase updates",
			task: model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "running", PhaseUpdatedAt: &justRefreshedPhase, SepiidaFirstReportExpectedAt: &startedLongAgo},
			want: true,
		},
		{
			name: "recent workflow start remains inside the deadline",
			task: model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "running", PhaseUpdatedAt: &oldPhaseUpdate, SepiidaFirstReportExpectedAt: &startedRecently},
			want: false,
		},
		{
			name: "archiving continues to enforce the original workflow start deadline",
			task: model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "archiving", PhaseUpdatedAt: &justRefreshedPhase, SepiidaFirstReportExpectedAt: &startedLongAgo},
			want: true,
		},
		{
			name: "Sepiida first report disables the first-report timeout",
			task: model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "running", SepiidaFirstReportExpectedAt: &startedLongAgo, SepiidaFirstReportedAt: &reported},
			want: false,
		},
		{
			name: "non-CVM execution does not use the Sepiida CVM timeout",
			task: model.Task{Executor: model.ExecutorLocal, ExecutionPhase: "running", SepiidaFirstReportExpectedAt: &startedLongAgo},
			want: false,
		},
		{
			name: "terminal phase does not use the first-report timeout",
			task: model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "terminal", SepiidaFirstReportExpectedAt: &startedLongAgo},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sepiidaFirstReportOverdue(&tt.task, timeout, now); got != tt.want {
				t.Fatalf("sepiidaFirstReportOverdue() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestSepiidaQueryOutageGraceEndsAtFortyMinutes(t *testing.T) {
	expectedAt := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	task := &model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "running", Status: model.TaskStatusRunning, SepiidaFirstReportExpectedAt: &expectedAt}
	timeout, grace := 10*time.Minute, 30*time.Minute
	if sepiidaQueryUnavailableOverdue(task, timeout, grace, expectedAt.Add(40*time.Minute-time.Second)) {
		t.Fatal("query outage should remain in grace immediately before 40 minutes")
	}
	if !sepiidaQueryUnavailableOverdue(task, timeout, grace, expectedAt.Add(40*time.Minute)) {
		t.Fatal("query outage should become terminal at expected_at + 40 minutes")
	}
	task.SepiidaFirstReportedAt = ptrTime(expectedAt.Add(time.Minute))
	if sepiidaQueryUnavailableOverdue(task, timeout, grace, expectedAt.Add(time.Hour)) {
		t.Fatal("a confirmed first progress report must disable the timeout")
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
