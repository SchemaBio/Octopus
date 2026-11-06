package service

import "testing"

func TestCalculateMTACMG(t *testing.T) {
	evidence := func(code, strength string) ACMGEvidenceEntry {
		return ACMGEvidenceEntry{Code: code, Strength: strength}
	}
	for _, test := range []struct {
		name string
		want string
		in   []ACMGEvidenceEntry
	}{
		{"stand-alone", "Benign", []ACMGEvidenceEntry{evidence("BA1", "stand_alone")}},
		{"benign-support", "Likely_Benign", []ACMGEvidenceEntry{evidence("BS3", "supporting"), evidence("BP4", "supporting")}},
		{"two-strong", "Pathogenic", []ACMGEvidenceEntry{evidence("PS1", "strong"), evidence("PS4", "strong")}},
		{"strong-and-support", "Likely_Pathogenic", []ACMGEvidenceEntry{evidence("PS1", "strong"), evidence("PM2", "supporting"), evidence("PP3", "supporting")}},
		{"discordant", "VUS", []ACMGEvidenceEntry{evidence("PS1", "strong"), evidence("BS1", "strong")}},
		{"nuclear-criterion", "", []ACMGEvidenceEntry{evidence("PM3", "moderate")}},
		{"wrong-strength", "", []ACMGEvidenceEntry{evidence("PM2", "moderate")}},
		{"duplicate", "", []ACMGEvidenceEntry{evidence("PM2", "supporting"), evidence("PM2", "supporting")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := CalculateMTACMG(test.in)
			if test.want == "" {
				if err == nil {
					t.Fatal("invalid evidence accepted")
				}
				return
			}
			if err != nil || result.Classification != test.want || result.Evidence[0].Source != "manual" {
				t.Fatalf("got %+v, %v; want %s", result, err, test.want)
			}
		})
	}
}
