package service

import "testing"

func TestCalculateSNVACMGPointBoundaries(t *testing.T) {
	tests := []struct {
		name           string
		evidence       []ACMGEvidenceEntry
		classification string
		score          int
	}{
		{"insufficient", nil, "", 0},
		{"vus", []ACMGEvidenceEntry{{Code: "PP3", Strength: "supporting"}}, "VUS", 1},
		{"likely pathogenic", []ACMGEvidenceEntry{{Code: "PS1", Strength: "strong"}, {Code: "PM1", Strength: "moderate"}}, "Likely_Pathogenic", 6},
		{"pathogenic", []ACMGEvidenceEntry{{Code: "PVS1", Strength: "very_strong"}, {Code: "PS1", Strength: "strong"}, {Code: "PM1", Strength: "moderate"}}, "Pathogenic", 14},
		{"likely benign", []ACMGEvidenceEntry{{Code: "BP4", Strength: "supporting"}}, "Likely_Benign", -1},
		{"benign", []ACMGEvidenceEntry{{Code: "BS1", Strength: "strong"}, {Code: "BP1", Strength: "supporting"}, {Code: "BP2", Strength: "supporting"}, {Code: "BP3", Strength: "supporting"}}, "Benign", -7},
		{"ba1", []ACMGEvidenceEntry{{Code: "BA1", Strength: "standalone"}}, "Benign", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := CalculateSNVACMG(test.evidence, "")
			if err != nil {
				t.Fatalf("CalculateSNVACMG returned error: %v", err)
			}
			if result.Score != test.score || result.Classification != test.classification {
				t.Fatalf("got score=%d classification=%q, want score=%d classification=%q", result.Score, result.Classification, test.score, test.classification)
			}
		})
	}
}

func TestCalculateSNVACMGRejectsDuplicateAndConflictingEvidence(t *testing.T) {
	tests := [][]ACMGEvidenceEntry{
		{{Code: "PP3", Strength: "supporting"}, {Code: "PP3", Strength: "moderate"}},
		{{Code: "PP3", Strength: "supporting"}, {Code: "BP4", Strength: "supporting"}},
		{{Code: "BA1", Strength: "standalone"}, {Code: "BS1", Strength: "strong"}},
	}
	for _, evidence := range tests {
		if _, err := CalculateSNVACMG(evidence, ""); err == nil {
			t.Fatalf("expected evidence set %v to be rejected", evidence)
		}
	}
}

func TestCalculateSNVACMGManualOverrideRequiresValidClass(t *testing.T) {
	result, err := CalculateSNVACMG([]ACMGEvidenceEntry{{Code: "PP3", Strength: "supporting"}}, "Pathogenic")
	if err != nil {
		t.Fatalf("valid manual override returned error: %v", err)
	}
	if result.Classification != "Pathogenic" || result.State != "manual_override" || result.Score != 1 {
		t.Fatalf("manual override lost evidence assessment: %#v", result)
	}
	if _, err := CalculateSNVACMG(nil, "Unknown"); err == nil {
		t.Fatal("expected invalid manual override to fail")
	}
}
