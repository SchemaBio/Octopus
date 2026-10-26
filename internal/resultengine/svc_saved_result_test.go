package resultengine

import (
	"encoding/json"
	"testing"
)

func TestSavedSVCAIProjectionPreservesProposalWithoutChangingClassification(t *testing.T) {
	ai := map[string]interface{}{"requestId": "saved-request", "summary": "Synthetic SVC summary", "resultKind": "experimental", "suggestedClassification": "Pathogenic"}
	p := projected{table: "snv-indel", overlay: map[string]interface{}{"svcv4AIResult": ai, "activeAcmgVersion": "legacy"}}
	var saved map[string]interface{}
	value, ok := p.field("svcv4AIResult").(string)
	if !ok || json.Unmarshal([]byte(value), &saved) != nil || saved["requestId"] != "saved-request" {
		t.Fatal("saved AI snapshot missing from query projection")
	}
	if p.field("activeAcmgVersion") != "legacy" || p.field("acmgClassification") != nil {
		t.Fatal("AI snapshot changed active classification")
	}
}
