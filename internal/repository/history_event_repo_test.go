package repository

import (
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestGroupEventsKeepsAttemptCountsAndUnknownTimes(t *testing.T) {
	known := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rows := []model.VariantReviewEvent{
		{TenantID: "org:acme", TaskUUID: "task-1", ExecutionAttemptID: "attempt-1", VariantType: "snv-indel", VariantFingerprint: "fp", HistoryGroupKey: "snv|hg38|GENE", Action: model.VariantReviewActionReviewed, OccurredAt: &known, TimestampKnown: true},
		{TenantID: "org:acme", TaskUUID: "task-2", ExecutionAttemptID: "attempt-2", VariantType: "snv-indel", VariantFingerprint: "fp", HistoryGroupKey: "snv|hg38|GENE", Action: model.VariantReviewActionReviewed, TimestampKnown: false},
	}
	groups := groupEvents(rows)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if groups[0].active != 2 {
		t.Fatalf("active count = %d, want 2", groups[0].active)
	}
	if !groups[0].unknown || groups[0].first == nil || groups[0].last == nil {
		t.Fatalf("expected known range plus unknown-time marker: %+v", groups[0])
	}
}

func TestGroupEventsIgnoresRevokedLatestProjection(t *testing.T) {
	rows := []model.VariantReviewEvent{
		{HistoryGroupKey: "snv|hg38|GENE", Action: model.VariantReviewActionRevoked},
	}
	groups := groupEvents(rows)
	if len(groups) != 1 || groups[0].active != 0 {
		t.Fatalf("revoked event should remain visible with zero active count: %+v", groups)
	}
}
