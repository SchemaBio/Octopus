package assistance

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func savedSVCFixture(t *testing.T) (*Service, Request) {
	t.Helper()
	block, err := aes.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	c, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Cipher: c}
	r := Request{ID: uuid.NewString(), Scope: "org:a", TaskID: "task", Feature: "svcv4", State: "completed"}
	r.Ciphertext, err = seal(c, r.ID, StoredInput{Input: Input{Feature: "svcv4", TaskID: "task", Variant: map[string]any{"id": strings.Repeat("a", 64)}}})
	if err != nil {
		t.Fatal(err)
	}
	r.ResultCiphertext, err = seal(c, r.ID+":result", Result{Feature: "svcv4", ResultKind: "experimental", Summary: "Synthetic SVC summary", SuggestedClassification: "VUS", Classification: "Pathogenic", RuleEngineResult: map[string]any{"classification": "Pathogenic"}, ReviewerDecision: map[string]any{"accepted": true}, Evidence: []map[string]string{{"code": "POP", "status": "insufficient_data", "reason": "Synthetic population evidence"}}, AgentProposal: map[string]any{"ai_assessment": map[string]any{"clinical_summary": "Synthetic SVC summary"}}, ExperimentalResult: map[string]any{"version": "draft", "total_points": nil}})
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}

func TestSavedSVCResultCopiesServerProposalWithoutClinicalAdoption(t *testing.T) {
	s, r := savedSVCFixture(t)
	saved, err := s.savedSVCResult(r, "task", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if saved["requestId"] != r.ID || saved["summary"] != "Synthetic SVC summary" || saved["suggestedClassification"] != "VUS" || saved["agentProposal"] == nil || saved["evidence"] == nil {
		t.Fatal("saved AI result incomplete")
	}
	for _, key := range []string{"classification", "ruleEngineResult", "reviewerDecision"} {
		if saved[key] != nil {
			t.Fatalf("clinical adoption leaked through %s", key)
		}
	}
}

func TestSavedSVCResultRejectsWrongVariantFeatureStateOrCorruptData(t *testing.T) {
	for _, scenario := range []string{"task", "row", "feature", "pending", "failed", "cancelled", "deleted", "corrupt_input", "corrupt_result"} {
		t.Run(scenario, func(t *testing.T) {
			s, r := savedSVCFixture(t)
			task, row := "task", strings.Repeat("a", 64)
			switch scenario {
			case "task":
				task = "other"
			case "row":
				row = strings.Repeat("b", 64)
			case "feature":
				r.Feature = "acmg"
			case "pending":
				r.State = "running"
			case "failed":
				r.State = "failed"
			case "cancelled":
				r.CancelRequested = true
			case "deleted":
				r.ResultCiphertext = ""
			case "corrupt_input":
				r.Ciphertext = "bad"
			case "corrupt_result":
				r.ResultCiphertext = "bad"
			}
			if _, err := s.savedSVCResult(r, task, row); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestSavedSVCResultScopedDatabaseLookup(t *testing.T) {
	s, _ := integrationService(t)
	fixture, r := savedSVCFixture(t)
	s.Cipher = fixture.Cipher
	if err := s.DB.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	row := strings.Repeat("a", 64)
	saved, err := s.SavedSVCResult(context.Background(), "org:a", "task", row, r.ID)
	if err != nil || saved["requestId"] != r.ID {
		t.Fatalf("save resolver failed: %v", err)
	}
	for _, pair := range [][2]string{{"org:b", "task"}, {"org:a", "other"}} {
		if _, err := s.SavedSVCResult(context.Background(), pair[0], pair[1], row, r.ID); err == nil {
			t.Fatal("cross-scope save accepted")
		}
	}
}

func TestSavedSVCCompletedLifecycleRetainsBindingAfterInputErasure(t *testing.T) {
	s, _ := integrationService(t)
	fixture, r := savedSVCFixture(t)
	s.Cipher = fixture.Cipher
	r.State = "running"
	r.ResultCiphertext = ""
	if err := s.DB.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	result := &Result{Feature: "svcv4", ResultKind: "experimental", Summary: "Synthetic completed SVC", SVCContext: map[string]any{"disease": map[string]any{"value": "Synthetic disease"}}}
	if err := s.finish(r, "completed", "", result, false); err != nil {
		t.Fatal(err)
	}
	var completed Request
	if err := s.DB.First(&completed, "id=?", r.ID).Error; err != nil {
		t.Fatal(err)
	}
	if completed.Ciphertext != "" {
		t.Fatal("clinical request input retained")
	}
	saved, err := s.SavedSVCResult(context.Background(), r.Scope, r.TaskID, strings.Repeat("a", 64), r.ID)
	if err != nil || saved["variantId"] != strings.Repeat("a", 64) || saved["svcContext"] == nil {
		t.Fatal("completed request cannot be saved", err)
	}
	if _, err := s.SavedSVCResult(context.Background(), r.Scope, r.TaskID, strings.Repeat("b", 64), r.ID); err == nil {
		t.Fatal("wrong row accepted")
	}
}
func TestLegacySVCVariantComparisonRequiresAllelesGeneAndTranscript(t *testing.T) {
	original := map[string]interface{}{"assembly": "GRCh37", "chrom": "1", "pos": float64(10), "ref": "A", "alt": "T", "gene": "G", "transcript": "NM_1"}
	row := map[string]interface{}{"assembly": "hg19", "chromosome": "chr1", "position": float64(10), "ref": "A", "alt": "T", "gene": "G", "transcript": "NM_1"}
	if !sameSVCVariant(original, row) {
		t.Fatal("matching original evidence rejected")
	}
	for _, field := range []string{"position", "ref", "alt", "gene", "transcript"} {
		old := row[field]
		row[field] = "different"
		if sameSVCVariant(original, row) {
			t.Fatalf("mismatch accepted: %s", field)
		}
		row[field] = old
	}
}

func TestSavedLegacySVCReadsOriginalEvidenceWithoutModelOrBilling(t *testing.T) {
	s, calls := integrationService(t)
	fixture, r := savedSVCFixture(t)
	s.Cipher = fixture.Cipher
	r.Ciphertext = ""
	r.JobID = "job-" + strings.Repeat("a", 32)
	original := map[string]interface{}{"assembly": "GRCh37", "chrom": "1", "pos": float64(10), "ref": "A", "alt": "T", "gene": "G", "transcript": "NM_1"}
	reads := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.Method != "GET" || q.URL.Path != "/v2/evaluations/"+r.JobID+"/evidence" {
			t.Error("unexpected upstream operation")
			w.WriteHeader(400)
			return
		}
		reads++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"evaluation_id": r.JobID, "annotations": map[string]interface{}{"engine_id": "svc_v4", "variant": original}})
	}))
	defer upstream.Close()
	s.Client = &Client{BaseURL: upstream.URL, Token: "synthetic", HTTP: upstream.Client()}
	r.UpstreamID = s.upstreamID()
	if err := s.DB.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	expected := map[string]interface{}{"assembly": "hg19", "chromosome": "1", "position": float64(10), "ref": "A", "alt": "T", "gene": "G", "transcript": "NM_1"}
	saved, err := s.SavedLegacySVCResult(context.Background(), r.Scope, r.TaskID, strings.Repeat("a", 64), r.ID, expected)
	if err != nil || saved["requestId"] != r.ID || reads != 1 || calls.Load() != 0 {
		t.Fatal("legacy result could not be saved without regeneration", err)
	}
	expected["transcript"] = "NM_OTHER"
	if _, err = s.SavedLegacySVCResult(context.Background(), r.Scope, r.TaskID, strings.Repeat("b", 64), r.ID, expected); err == nil {
		t.Fatal("wrong transcript accepted")
	}
}
