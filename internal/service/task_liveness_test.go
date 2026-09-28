package service

import (
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestNodeAndAgentLivenessUseIndependentThresholds(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	last := now.Add(-time.Minute)
	if got := nodeLiveness(&last, now).State; got != "online" {
		t.Fatalf("node at one minute should be online, got %q", got)
	}
	last = now.Add(-3 * time.Minute)
	if got := nodeLiveness(&last, now).State; got != "delayed" {
		t.Fatalf("node at three minutes should be delayed, got %q", got)
	}
	last = now.Add(-6 * time.Minute)
	if got := nodeLiveness(&last, now).State; got != "offline" {
		t.Fatalf("node at six minutes should be offline, got %q", got)
	}

	last = now.Add(-40 * time.Second)
	session := &model.SepiidaAgentSession{LastCollectedAt: &last, CollectionIntervalSeconds: 15, LastCollectionStatus: "error", LastErrorCode: "collection_failed"}
	status := agentLiveness(session, now)
	if status.State != "online" || status.CollectionStatus != "error" || status.ErrorCode != "collection_failed" {
		t.Fatalf("agent liveness and collection outcome should remain separate: %+v", status)
	}
	last = now.Add(-time.Minute)
	if got := agentLiveness(&model.SepiidaAgentSession{LastCollectedAt: &last, CollectionIntervalSeconds: 15}, now).State; got != "delayed" {
		t.Fatalf("agent at one minute should be delayed, got %q", got)
	}
	last = now.Add(-3 * time.Minute)
	if got := agentLiveness(&model.SepiidaAgentSession{LastCollectedAt: &last, CollectionIntervalSeconds: 15}, now).State; got != "offline" {
		t.Fatalf("agent at three minutes should be offline, got %q", got)
	}
	if got := agentLiveness(&model.SepiidaAgentSession{LastProgressPushAt: &now}, now).State; got != "legacy/unknown" {
		t.Fatalf("progress-only legacy Agent should be unknown for collection liveness, got %q", got)
	}
	last = now.Add(-2 * time.Minute)
	if got := agentLiveness(&model.SepiidaAgentSession{LastCollectedAt: &last, CollectionIntervalSeconds: 60}, now).State; got != "online" {
		t.Fatalf("larger collection interval should scale online threshold, got %q", got)
	}
}
