package service

import (
	"github.com/google/uuid"
	"testing"
)

func TestCNVAIReferenceRejectsUnboundBrowserContent(t *testing.T) {
	id := uuid.NewString()
	for _, v := range []interface{}{id, map[string]interface{}{"requestId": id, "classification": "forged", "summary": "forged"}} {
		got, err := cnvAIRequestID(v)
		if err != nil || got != id {
			t.Fatal(got, err)
		}
	}
	for _, v := range []interface{}{nil, "", map[string]interface{}{"summary": "forged"}, map[string]interface{}{"requestId": 123}} {
		if _, err := cnvAIRequestID(v); err == nil {
			t.Fatal("unbound browser content accepted")
		}
	}
}

func TestCNVAdoptionUsesServerEvidence(t *testing.T) {
	payload := map[string]interface{}{"adoptedCNV": map[string]interface{}{"source": "ai", "classification": "Pathogenic", "score": 99, "savedBy": "forged"}, "cnvAIResult": map[string]interface{}{"requestId": "server-request", "suggestedClassification": "VUS"}}
	if err := canonicalCNVAdoption(payload, "reviewer"); err != nil {
		t.Fatal(err)
	}
	saved := payload["adoptedCNV"].(map[string]interface{})
	if saved["classification"] != "VUS" || saved["score"] != nil || saved["savedBy"] != "reviewer" {
		t.Fatal(saved)
	}
	for _, source := range []string{"ai", "clingen", "unknown"} {
		if canonicalCNVAdoption(map[string]interface{}{"adoptedCNV": map[string]interface{}{"source": source}}, "reviewer") == nil {
			t.Fatal("missing evidence accepted", source)
		}
	}
}
