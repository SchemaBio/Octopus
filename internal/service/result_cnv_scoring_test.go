package service

import (
	"encoding/json"
	"testing"
)

func cnvScoreFixture(t *testing.T, kind string) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	raw := `{"cnvType":"` + kind + `","criteria":{"section1":{"selected":null},"section2":{"hiOverlap":{"selected":null,"score":0},"benignOverlap":{"selected":null,"score":0}},"section3":{"confirmed":false,"geneCount":0},"section4":{"deNovo":{},"unknownInheritance":{},"segregation":{},"nonSegregation":{},"caseControl":{}},"section5":{"other":{"selected":null,"score":0}}},"totalScore":100,"classification":"Pathogenic"}`
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestCNVSaveRecomputesRatherThanTrustingClassification(t *testing.T) {
	p := cnvScoreFixture(t, "Deletion")
	if err := normalizeCNVScoring(p); err != nil {
		t.Fatal(err)
	}
	if p["classification"] != "" || p["totalScore"] != float64(0) || p["assessmentState"] != "insufficient_evidence" {
		t.Fatal("empty evidence treated as a classification")
	}
	p["cnvId"] = "row"
	raw, _ := json.Marshal(p)
	if err := validateCNVAssessmentPayload(raw, "row"); err != nil {
		t.Fatalf("empty evidence cannot be saved again: %v", err)
	}
	c := p["criteria"].(map[string]interface{})
	c["section2"].(map[string]interface{})["hiOverlap"] = map[string]interface{}{"selected": "2A", "score": 1.0}
	if err := normalizeCNVScoring(p); err != nil {
		t.Fatal(err)
	}
	if p["classification"] != "Pathogenic" || p["totalScore"] != 1.0 {
		t.Fatal("official complete dosage score mismatch")
	}
}
func TestCNVScoringLossGainCodingCounts(t *testing.T) {
	for _, test := range []struct {
		kind  string
		genes int
		want  float64
	}{{"Deletion", 24, 0}, {"Deletion", 25, .45}, {"Deletion", 35, .9}, {"Amplification", 34, 0}, {"Amplification", 35, .45}, {"Amplification", 50, .9}} {
		p := cnvScoreFixture(t, test.kind)
		p["criteria"].(map[string]interface{})["section3"] = map[string]interface{}{"confirmed": true, "geneCount": test.genes}
		if err := normalizeCNVScoring(p); err != nil {
			t.Fatal(err)
		}
		if p["totalScore"] != test.want {
			t.Fatalf("%s %d = %v", test.kind, test.genes, p["totalScore"])
		}
	}
}
func TestCNVScoringRejectsConflictingAndOutOfRangeEvidence(t *testing.T) {
	p := cnvScoreFixture(t, "Deletion")
	two := p["criteria"].(map[string]interface{})["section2"].(map[string]interface{})
	two["hiOverlap"] = map[string]interface{}{"selected": "2A", "score": 9}
	if normalizeCNVScoring(p) == nil {
		t.Fatal("invalid score accepted")
	}
	two["hiOverlap"] = map[string]interface{}{"selected": "2A", "score": 1}
	two["benignOverlap"] = map[string]interface{}{"selected": "2F", "score": -1}
	if normalizeCNVScoring(p) == nil {
		t.Fatal("conflicting dosage evidence accepted")
	}
}

func TestCNVCalculatorMatchesImmutableEvent(t *testing.T) {
	for _, source := range []string{"DEL", "deletion", "LOSS"} {
		if err := verifyCNVEventType(map[string]interface{}{"Col4": source}, map[string]interface{}{"cnvType": "Deletion"}); err != nil {
			t.Fatal(err)
		}
	}
	if verifyCNVEventType(map[string]interface{}{"Col4": "DUP"}, map[string]interface{}{"cnvType": "Deletion"}) == nil {
		t.Fatal("gain was assessed as loss")
	}
	if verifyCNVEventType(map[string]interface{}{"Col4": "Normal"}, map[string]interface{}{"cnvType": "Deletion"}) == nil {
		t.Fatal("normal signal was assessed as an event")
	}
}

func TestCNVOfficialSegregationBuckets(t *testing.T) {
	for _, test := range []struct {
		code string
		want float64
	}{{"4F", .15}, {"4G", .30}, {"4H", .45}} {
		p := cnvScoreFixture(t, "Deletion")
		p["criteria"].(map[string]interface{})["section4"].(map[string]interface{})["segregation"] = map[string]interface{}{test.code: 1}
		if err := normalizeCNVScoring(p); err != nil {
			t.Fatal(err)
		}
		if p["totalScore"] != test.want {
			t.Fatalf("%s mismatch: %v", test.code, p["totalScore"])
		}
	}
	p := cnvScoreFixture(t, "Amplification")
	p["criteria"].(map[string]interface{})["section2"].(map[string]interface{})["hiOverlap"] = map[string]interface{}{"selected": "2A", "score": 1}
	if normalizeCNVScoring(p) == nil {
		t.Fatal("gain HI was scored as TS")
	}
}
