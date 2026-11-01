package assistance

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestSavedACMGResultPinsTaskVariantDatasetAndKeepsProposalSeparate(t *testing.T) {
	s, r := savedSVCFixture(t)
	r.Feature = "acmg"
	result := Result{Feature: "acmg", VariantID: strings.Repeat("a", 64), DatasetHash: strings.Repeat("b", 64), Summary: "synthetic evidence", ResultKind: "evidence_only", SuggestedClassification: "VUS", ReviewerDecision: map[string]any{"confirmed": true}, CalculatorDraft: &CalculatorDraft{Version: "acmg", Changes: []CalculatorChange{{Path: "acmgEvidence.PP3", Status: "insufficient_data"}}}}
	var err error
	r.ResultCiphertext, err = seal(s.Cipher, r.ID+":result", result)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.savedACMGResult(r, "task", result.VariantID, result.DatasetHash)
	if err != nil || saved["requestId"] != r.ID || saved["reviewerDecision"] != nil || saved["calculatorDraft"] == nil {
		t.Fatal(saved, err)
	}
	for _, args := range [][3]string{{"other", result.VariantID, result.DatasetHash}, {"task", "other", result.DatasetHash}, {"task", result.VariantID, "other"}} {
		if _, err := s.savedACMGResult(r, args[0], args[1], args[2]); err == nil {
			t.Fatal("mismatched AI archive accepted")
		}
	}
}

func TestCalculatorArchivePostgresRejectsOtherUserTaskAndFeature(t *testing.T) {
	s, calls := integrationService(t)
	r := Request{ID: uuid.NewString(), Scope: "org:fixture", TaskID: "task", UserID: 7, Feature: "acmg", State: "completed", Key: "fixed"}
	if e := s.DB.Create(&r).Error; e != nil {
		t.Fatal(e)
	}
	if !s.CanSaveCalculatorResult(context.Background(), r.Scope, r.TaskID, 7, r.ID, "acmg") {
		t.Fatal("owned result rejected")
	}
	for _, c := range []struct {
		scope, task, feature string
		user                 uint
	}{{r.Scope, r.TaskID, "acmg", 8}, {"other", r.TaskID, "acmg", 7}, {r.Scope, "other", "acmg", 7}, {r.Scope, r.TaskID, "svcv4", 7}} {
		if s.CanSaveCalculatorResult(context.Background(), c.scope, c.task, c.user, r.ID, c.feature) {
			t.Fatal("foreign archive accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("archive authorization invoked model")
	}
}
