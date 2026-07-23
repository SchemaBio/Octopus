package model

import (
	"encoding/json"
	"testing"
)

func TestSampleClinicalDiagnosisUsesValidJSON(t *testing.T) {
	var sample Sample
	sample.SetClinicalDiagnosis("遗传性心肌病待查")
	if !json.Valid([]byte(sample.ClinicalDiagnosis)) {
		t.Fatalf("clinical diagnosis is not valid JSON: %q", sample.ClinicalDiagnosis)
	}
	if got := sample.GetClinicalDiagnosis(); got != "遗传性心肌病待查" {
		t.Fatalf("GetClinicalDiagnosis() = %q", got)
	}
}

func TestSampleNilMatchedPairUsesJSONNull(t *testing.T) {
	var sample Sample
	sample.SetMatchedPair(nil)
	if sample.MatchedPair != "null" {
		t.Fatalf("nil matched pair = %q, want JSON null", sample.MatchedPair)
	}
	if sample.GetMatchedPair() != nil {
		t.Fatal("JSON null matched pair should remain nil")
	}
}
