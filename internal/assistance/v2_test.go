package assistance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestV2AIOnlyPayloadAndUntrustedAnnotations(t *testing.T) {
	for _, feature := range []string{"acmg", "cnv_gain", "cnv_loss"} {
		input := Input{Feature: feature, Reference: "hg38", Variant: map[string]any{"chromosome": "chr1", "position": float64(100), "ref": "A", "alt": "G", "gene": "TEST", "coordinateSystem": "1-based-inclusive", "startPosition": float64(100), "endPosition": float64(200), "gnomadAF": 0.001, "sample_id": "private"}, ClinicalContext: Context{DiseaseID: "MONDO:1", Inheritance: "AD", SampleType: "blood", VariantOrigin: "germline_confirmed"}}
		path, p, err := BuildPayload(input, "profile")
		if err != nil || path != "/v2/evaluations" || p["mode"] != "ai_assisted" || p["schema_version"] != "sepilens-evaluation-2.0" {
			t.Fatal(path, p, err)
		}
		raw, _ := json.Marshal(p)
		var wire map[string]any
		_ = json.Unmarshal(raw, &wire)
		pkg := wire["evidence_package"].(map[string]any)
		if len(pkg["sources"].([]any)) != 0 || pkg["criteria"] != nil || p["sources"] != nil {
			t.Fatal("invented provenance or applied calculator evidence", p)
		}
	}
	if path, payload, err := BuildPayload(Input{Feature: "hpo", Diagnosis: "private"}, "profile"); err != nil || path != "/v2/hpo/extract" || payload["options"].(map[string]any)["allow_local_fallback"] != false {
		t.Fatal(path, payload, err)
	}
}

func TestV2ResultDoesNotDeliverFailedLocalBaseline(t *testing.T) {
	raw := map[string]any{"schema_version": "sepilens-evaluation-2.0", "delivery_status": "failed", "failure_code": "AI_PROVIDER_UNAVAILABLE", "local_baseline": map[string]any{"classification": "pathogenic"}}
	if _, _, err := NormalizeResult("acmg", "m", raw); err == nil {
		t.Fatal("failed AI baseline delivered")
	}
	raw["delivery_status"] = "delivered"
	raw["failure_code"] = ""
	if _, _, err := NormalizeResult("acmg", "m", raw); err == nil {
		t.Fatal("local baseline masqueraded as AI result")
	}
	raw["agent_proposal"] = map[string]any{"ai_assessment": map[string]any{"criteria": []any{map[string]any{"code": "PM2", "override_reason": "frequency requires verification"}}}}
	raw["rule_engine_result"] = map[string]any{"classification": "insufficient_data"}
	result, bill, err := NormalizeResult("acmg", "m", raw)
	if err != nil || bill || len(result.Evidence) != 1 || result.Evidence[0]["code"] != "PM2" {
		t.Fatal(result, bill, err)
	}
}

func TestV2JobIdentifiers(t *testing.T) {
	for _, id := range []string{"job-0123456789abcdef0123456789abcdef"} {
		if !validJobID(id) {
			t.Fatal(id)
		}
	}
	for _, id := range []string{"../providers/test", "job-nothex", "01234567-89ab-cdef-0123-456789abcdef"} {
		if validJobID(id) {
			t.Fatal(id)
		}
	}
}

func TestACMGTaskContextAndStructuredAIResult(t *testing.T) {
	in := Input{Feature: "acmg", Reference: "hg38", TaskID: "private-task", Variant: map[string]any{"chromosome": "chr1", "position": float64(100), "ref": "A", "alt": "G", "gene": "TEST", "gnomadAF": 0.0, "clinvarSignificance": "Uncertain significance", "clinvarReviewStatus": "criteria provided", "clinvarId": "123"}, ClinicalContext: Context{DiseaseID: "MONDO:0000001", Inheritance: "AD", SampleType: "blood", HPOTerms: []string{"HP:0001250"}}}
	_, payload, err := BuildPayload(in, "profile")
	if err != nil {
		t.Fatal(err)
	}
	if payload["variant"].(map[string]any)["allele_frequency"] != 0.0 {
		t.Fatal("zero AF dropped")
	}
	if payload["clinical_context"].(map[string]any)["inheritance"] != "unknown" {
		t.Fatal("invented inheritance")
	}
	pkg := payload["evidence_package"].(map[string]any)
	annotations := pkg["annotations"].(map[string]any)
	if annotations["gnomad"] != nil || annotations["clinvar"].(map[string]any)["status"] != "caller_supplied" {
		t.Fatal("invented population AF or trusted annotation")
	}
	encoded, _ := json.Marshal(payload)
	if strings.Contains(string(encoded), "private-task") {
		t.Fatal("task ID sent upstream")
	}
	r, bill, err := NormalizeResult("acmg", "synthetic", map[string]any{"schema_version": "sepilens-evaluation-2.0", "delivery_status": "delivered", "result_kind": "evidence_only", "agent_proposal": map[string]any{"ai_assessment": map[string]any{"classification": "VUS", "criteria": []any{map[string]any{"code": "PM2", "status": "insufficient_data", "strength": "Supporting", "override_reason": "coverage missing"}}}}})
	if err != nil || bill || r.Classification != "" || r.SuggestedClassification != "VUS" || r.Evidence[0]["status"] != "insufficient_data" || r.Evidence[0]["strength"] != "Supporting" {
		t.Fatal(r, bill, err)
	}
}

func TestACMGNeedsNoManualContextAndPreservesAllClinicalAnnotations(t *testing.T) {
	in := Input{Feature: "acmg", Reference: "hg38", Variant: map[string]any{"gene": "TEST1;TEST2", "chromosome": "1", "position": float64(100), "ref": "A", "alt": "G", "id": "private-row", "annotationValues": map[string]any{"SIFT_pred": "deleterious", "GT": "0/1", "API_KEY": "private", "Sample_ID": "private"}}, SampleContext: map[string]any{"gender": "female", "hpo_terms": []any{map[string]any{"id": "HP:0001250"}}}}
	_, p, err := BuildPayload(in, "profile")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "private") || !strings.Contains(string(raw), "SIFT_pred") || !strings.Contains(string(raw), "HP:0001250") {
		t.Fatal("clinical context omitted or operational metadata included")
	}
	if p["variant"].(map[string]any)["gene"] != "" {
		t.Fatal("multiple genes treated as one canonical gene")
	}
	if p["clinical_context"].(map[string]any)["disease_id"] != "" {
		t.Fatal("invented disease ID")
	}
}

func TestV2HPOAIExtractionDoesNotPersistOriginal(t *testing.T) {
	s, calls := integrationService(t)
	r, err := s.Submit(context.Background(), "org:a", "a", 1, "hpo-memory", Input{Feature: "hpo", Diagnosis: "private phenotype text"})
	if err != nil {
		t.Fatal(err)
	}
	var stored StoredInput
	if err = open(s.Cipher, r.ID, r.Ciphertext, &stored); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(stored)
	if strings.Contains(string(raw), "private phenotype text") || stored.Payload["text"] != nil {
		t.Fatal("original persisted")
	}
	s.process(context.Background(), *r)
	current, err := s.Get("org:a", 1, r.ID)
	if err != nil || current.State != "completed" || calls.Load() != 1 {
		t.Fatal(current.State, calls.Load(), err)
	}
	if _, ok := getHPO(r.ID); ok {
		t.Fatal("original retained after extraction")
	}
	view, err := s.View(current)
	if err != nil || len(view.Result.HPOTerms) != 1 {
		t.Fatal(view, err)
	}
}
