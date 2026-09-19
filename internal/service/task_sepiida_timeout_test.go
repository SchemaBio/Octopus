package service

import (
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

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
