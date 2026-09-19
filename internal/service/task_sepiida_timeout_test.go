package service

import (
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestSepiidaFirstReportOverdue(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-11 * time.Minute)
	task := &model.Task{Executor: model.ExecutorCVM, ExecutionPhase: "bootstrapping", PhaseUpdatedAt: &old}
	if !sepiidaFirstReportOverdue(task, 10*time.Minute, now) {
		t.Fatal("missing first report did not time out")
	}
	reported := now.Add(-time.Minute)
	task.SepiidaFirstReportedAt = &reported
	if sepiidaFirstReportOverdue(task, 10*time.Minute, now) {
		t.Fatal("an observed Sepiida execution must never use the first-report timeout")
	}
	task.SepiidaFirstReportedAt = nil
	task.ExecutionPhase = "archiving"
	if sepiidaFirstReportOverdue(task, 10*time.Minute, now) {
		t.Fatal("archive phase must not be treated as a missing first report")
	}
}
