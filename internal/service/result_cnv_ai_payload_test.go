package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCNVAISnapshotPayloadBound(t *testing.T) {
	p := cnvScoreFixture(t, "Deletion")
	p["cnvId"] = "row"
	p["criteria"].(map[string]interface{})["aiReview"] = map[string]interface{}{"requestId": "synthetic", "summary": strings.Repeat("x", 70*1024)}
	raw, _ := json.Marshal(p)
	if e := validateCNVAssessmentPayload(raw, "row"); e != nil {
		t.Fatal("bounded AI audit cannot be saved", e)
	}
	p["extra"] = strings.Repeat("x", 2*1024*1024)
	raw, _ = json.Marshal(p)
	if validateCNVAssessmentPayload(raw, "row") == nil {
		t.Fatal("oversized snapshot accepted")
	}
}
