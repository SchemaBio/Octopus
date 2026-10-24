package assistance

import (
	"context"
	"testing"
	"time"
)

func TestCachedHPOPersistsAcrossServiceInstancesWithoutModelCalls(t *testing.T) {
	s, calls := integrationService(t)
	ctx := context.Background()
	input := Input{Feature: "hpo", Diagnosis: "synthetic phenotype"}
	request, err := s.Submit(ctx, "org:a", "a", 1, "first-hpo", input)
	if err != nil {
		t.Fatal(err)
	}
	s.process(ctx, *request)
	restarted := &Service{DB: s.DB, Cipher: s.Cipher}
	result, err := restarted.CachedHPO(ctx, "org:a", 1, input.Diagnosis)
	if err != nil || result == nil || len(result.HPOTerms) != 1 || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
	for _, other := range []struct {
		scope string
		user  uint
		text  string
	}{{"org:b", 1, input.Diagnosis}, {"org:a", 2, input.Diagnosis}, {"org:a", 1, "different phenotype"}} {
		result, err := restarted.CachedHPO(ctx, other.scope, other.user, other.text)
		if err != nil || result != nil {
			t.Fatal("cache crossed user, organization or input boundary", result, err)
		}
	}
	if err = s.DB.Model(request).Update("updated_at", time.Now().Add(-90*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	s.purgeExpired(ctx)
	if result, err = s.CachedHPO(ctx, "org:a", 1, input.Diagnosis); err != nil || result == nil {
		t.Fatal("saved HPO unexpectedly expired", err)
	}
	failed, err := s.Submit(ctx, "org:a", "a", 1, "failed-regeneration", input)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.finish(*failed, "failed", "provider_unavailable", nil, false); err != nil {
		t.Fatal(err)
	}
	if result, err = s.CachedHPO(ctx, "org:a", 1, input.Diagnosis); err != nil || result == nil {
		t.Fatal("failed regeneration erased previous result", err)
	}
	newer, err := s.Submit(ctx, "org:a", "a", 1, "explicit-regeneration", input)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.finish(*newer, "completed", "", &Result{Feature: "hpo", Summary: "new generation"}, false); err != nil {
		t.Fatal(err)
	}
	result, err = s.CachedHPO(ctx, "org:a", 1, input.Diagnosis)
	if err != nil || result == nil || result.Summary != "new generation" || calls.Load() != 1 {
		t.Fatal("latest explicitly generated result not restored", result, err)
	}
}
