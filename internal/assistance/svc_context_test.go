package assistance

import "testing"

func TestSVCAIUsesImportedContextWithoutManualDraft(t *testing.T) {
	sample := map[string]any{"gender": "male", "age": "10"}
	_, body, err := BuildPayload(Input{Feature: "svcv4", Mode: "experimental", Reference: "hg38", SampleContext: sample, Variant: map[string]any{"gene": "A&B", "chromosome": "1", "position": float64(1), "ref": "A", "alt": "G", "annotationValues": map[string]any{"Consequence": "stop_gained", "patient_name": "private"}}}, "profile")
	if err != nil {
		t.Fatal(err)
	}
	pkg := body["evidence_package"].(map[string]any)
	ctx := pkg["ai_context"].(map[string]any)
	if ctx["sample"].(map[string]any)["gender"] != "male" || pkg["draft_assessment"] != nil {
		t.Fatal("sample missing or unverified method forwarded")
	}
	columns := ctx["variant_annotations"].(map[string]any)["annotationValues"].(map[string]any)
	if columns["Consequence"] != "stop_gained" || columns["patient_name"] != nil {
		t.Fatal("annotation allowlist changed")
	}
	if body["variant"].(map[string]any)["gene"] != "" {
		t.Fatal("multi-gene label became a canonical gene")
	}
}

func TestSVCNormalizationKeepsAISummaryExperimental(t *testing.T) {
	raw := map[string]any{"schema_version": "sepilens-evaluation-2.0", "delivery_status": "delivered", "result_kind": "experimental", "settlement_eligible": true, "agent_proposal": map[string]any{"experimental_result": map[string]any{"status": "insufficient_data"}, "ai_assessment": map[string]any{"clinical_summary": "synthetic organized context", "classification": "VUS", "criteria": []any{map[string]any{"code": "PFD", "status": "insufficient_data", "override_reason": "missing prerequisites"}}}}}
	result, bill, err := NormalizeResult("svcv4", "model", raw)
	if err != nil || bill || result.Summary != "synthetic organized context" || len(result.Evidence) != 1 || result.Classification != "" || result.RuleEngineResult != nil || result.ResultKind != "experimental" {
		t.Fatal("SVC summary or experimental boundaries lost", err)
	}
}
