package assistance

import (
	"encoding/json"
	"testing"
)

func TestFactPackWireExtensionIsServerControlledAndOffByDefault(t *testing.T) {
	t.Setenv("AI_ASSISTANCE_EVIDENCE_CHAIN_ENABLED", "")
	if New(nil, false, "", "").EvidenceChain {
		t.Fatal("evidence transport unexpectedly enabled")
	}
	t.Setenv("AI_ASSISTANCE_EVIDENCE_CHAIN_ENABLED", "true")
	if !New(nil, false, "", "").EvidenceChain {
		t.Fatal("explicit opt-in ignored")
	}
	var input Input
	if err := json.Unmarshal([]byte(`{"feature":"acmg","reference":"hg38","variant":{"chromosome":"1","position":100,"ref":"A","alt":"G"},"IncludeFactPack":true,"includeFactPack":true}`), &input); err != nil {
		t.Fatal(err)
	}
	if input.IncludeFactPack {
		t.Fatal("browser enabled wire extension")
	}
	_, body, err := BuildPayload(input, "synthetic-profile")
	if err != nil {
		t.Fatal(err)
	}
	ai := body["evidence_package"].(map[string]any)["ai_context"].(map[string]any)
	if _, exists := ai["fact_pack"]; exists {
		t.Fatal("default payload incompatible with old SepiLens")
	}
}
