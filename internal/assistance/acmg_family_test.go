package assistance

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestACMGTrioCallsRemainNamedUnverifiedContext(t *testing.T) {
	calls := map[string]any{"proband": map[string]any{"genotype": "1|0", "alleleDepth": "63,68", "alleleFrequency": float64(68) / 131, "sampleId": "private"}, "father": map[string]any{"genotype": "0/0", "alleleDepth": "53,0", "alleleFrequency": float64(0)}, "mother": map[string]any{"genotype": "0/1", "alleleDepth": "58,59", "alleleFrequency": float64(59) / 117}, "patient_id": "private"}
	_, body, e := BuildPayload(Input{Feature: "acmg", Reference: "hg19", SampleContext: map[string]any{"analysis_mode": "trio"}, Variant: map[string]any{"chromosome": "1", "position": float64(65099775), "ref": "C", "alt": "T", "sampleGenotypes": calls}}, "profile")
	if e != nil {
		t.Fatal(e)
	}
	ai := body["evidence_package"].(map[string]any)["ai_context"].(map[string]any)
	actual := ai["variant_annotations"].(map[string]any)["sampleGenotypes"].(map[string]any)
	if len(actual) != 3 || actual["father"].(map[string]any)["alleleFrequency"] != float64(0) || actual["proband"].(map[string]any)["genotype"] != "1|0" {
		t.Fatal("role or zero VAF lost", actual)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "private") {
		t.Fatal("identifier leaked")
	}
	if body["mode"] != "ai_assisted" || body["clinical_context"].(map[string]any)["variant_origin"] != "unknown" {
		t.Fatal("execution mode or germline origin inferred")
	}
	missing := acmgSampleGenotypes(map[string]any{"father": map[string]any{"zygosity": "Unknown", "alleleFrequency": "missing", "genotype": "./."}})
	if _, ok := missing["father"].(map[string]any)["alleleFrequency"]; ok {
		t.Fatal("missing VAF converted to zero")
	}
}
