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

func TestSampleManualMatchedPairOverridesAutomaticAndCanBeCleared(t *testing.T) {
	var sample Sample
	automatic := &MatchedPair{R1Path: "auto_R1.fastq.gz", R2Path: "auto_R2.fastq.gz"}
	manual := &MatchedPair{R1Path: "manual_R1.fastq.gz", R2Path: "manual_R2.fastq.gz"}
	sample.SetAutoMatchedPair(automatic)
	sample.SetMatchedPair(manual)

	if got := sample.GetMatchedPair(); got == nil || got.R1Path != manual.R1Path {
		t.Fatalf("effective pair = %#v, want manual pair", got)
	}

	sample.SetMatchedPair(nil)
	if got := sample.GetMatchedPair(); got == nil || got.R1Path != automatic.R1Path {
		t.Fatalf("effective pair after clearing manual = %#v, want automatic pair", got)
	}
}
