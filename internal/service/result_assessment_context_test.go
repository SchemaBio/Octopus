package service

import (
	"os"
	"strings"
	"testing"
)

func TestAssessmentPublishedPackage(t *testing.T) {
	filename := os.Getenv("ASSESSMENT_TEST_REFERENCE")
	if filename == "" {
		t.Skip("set ASSESSMENT_TEST_REFERENCE to validate an administrator release")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateAssessmentPack(raw, "hg19"); err != nil {
		t.Fatal(err)
	}
}

func TestAssessmentHPOExcludesNegatedAndInvalidTerms(t *testing.T) {
	terms := positiveAssessmentHPO(`[{"id":"HP:0000002"},{"id":"HP:0000002"},{"id":"HP:0000003","negated":true},{"id":"HP:0000004","excluded":true},{"id":"HP:0000005","qualifier":"NOT"},{"id":"HP:bad"},{"id":"HP:0000001"}]`)
	if len(terms) != 2 || terms[0] != "HP:0000001" || terms[1] != "HP:0000002" {
		t.Fatalf("unexpected positive terms: %v", terms)
	}
	if len(positiveAssessmentHPO("invalid")) != 0 {
		t.Fatal("invalid JSON must not create phenotype evidence")
	}
}

func TestAssessmentPackRejectsIncompleteAndWrongReferenceResources(t *testing.T) {
	raw := `{"hpo":{"HP:0000001":{"id":"HP:0000001","ic":4,"ancestors":[]}},"diseases":[{"id":"D","gene":"G","hpo":["HP:0000001"],"validity":"Strong","inheritance":"AD"}],"dosage":[{"reference":"hg19","chromosome":"1","start":0,"end":100}],"str":[],"imprinting":[]}`
	if err := validateAssessmentPack([]byte(raw), "hg19"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `"str":[]`, `"str":null`, 1), strings.Replace(raw, `"reference":"hg19"`, `"reference":"hg38"`, 1), strings.Replace(raw, `"ic":4`, `"ic":-1`, 1), strings.Replace(raw, `"ancestors":[]`, `"ancestors":["HP:9999999"]`, 1), strings.Replace(raw, `"end":100`, `"end":0`, 1), strings.Replace(raw, `"validity":"Strong"`, `"validity":"Disputed"`, 1)} {
		if validateAssessmentPack([]byte(bad), "hg19") == nil {
			t.Fatal("invalid resource accepted")
		}
	}
}
