package service

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ClinGen Mitochondrial Diseases VCEP mtDNA v1.0, with ACMG/AMP Table 5
// combinations. The reviewer supplies the applicable criteria and strengths.
var mtACMGStrengths = map[string][]string{
	"PVS1": {"very_strong", "strong", "moderate", "supporting"},
	"PS1":  {"strong"},
	"PS2":  {"very_strong", "strong", "moderate", "supporting"},
	"PS3":  {"supporting"},
	"PS4":  {"strong", "moderate", "supporting"},
	"PM2":  {"supporting"},
	"PM4":  {"moderate"},
	"PM5":  {"moderate", "supporting"},
	"PM6":  {"very_strong", "strong", "moderate", "supporting"},
	"PP1":  {"moderate", "supporting"},
	"PP3":  {"supporting"},
	"PP4":  {"supporting"},
	"BA1":  {"stand_alone"},
	"BS1":  {"strong"},
	"BS2":  {"strong", "supporting"},
	"BS3":  {"supporting"},
	"BS4":  {"strong"},
	"BP2":  {"supporting"},
	"BP4":  {"supporting"},
	"BP5":  {"supporting"},
	"BP7":  {"supporting"},
}

type mtACMGAssessment struct {
	Profile        string              `json:"profile"`
	Classification string              `json:"classification"`
	Basis          string              `json:"basis"`
	Evidence       []ACMGEvidenceEntry `json:"evidence"`
}

func CalculateMTACMG(raw interface{}) (*mtACMGAssessment, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid mtDNA ACMG evidence")
	}
	var evidence []ACMGEvidenceEntry
	if err := json.Unmarshal(data, &evidence); err != nil || len(evidence) == 0 || len(evidence) > len(mtACMGStrengths) {
		return nil, fmt.Errorf("select at least one applicable mtDNA ACMG criterion")
	}
	pathogenic, benign := map[string]int{}, map[string]int{}
	seen := make(map[string]bool)
	for i := range evidence {
		entry := &evidence[i]
		if seen[entry.Code] || !slices.Contains(mtACMGStrengths[entry.Code], entry.Strength) {
			return nil, fmt.Errorf("duplicate or unsupported mtDNA ACMG criterion or strength")
		}
		if len(entry.Note) > 4000 {
			return nil, fmt.Errorf("mtDNA ACMG evidence note is too long")
		}
		seen[entry.Code] = true
		entry.Source = "manual"
		if strings.HasPrefix(entry.Code, "B") {
			benign[entry.Strength]++
		} else {
			pathogenic[entry.Strength]++
		}
	}
	if (seen["PS2"] && seen["PM6"]) || (seen["PP3"] && seen["BP4"]) ||
		(seen["PS3"] && seen["BS3"]) || (seen["BA1"] && seen["BS1"]) {
		return nil, fmt.Errorf("overlapping or mutually exclusive mtDNA ACMG criteria")
	}
	vs, s, m, p := pathogenic["very_strong"], pathogenic["strong"], pathogenic["moderate"], pathogenic["supporting"]
	bs, bp := benign["strong"], benign["supporting"]
	classification := "VUS"
	basis := "insufficient"
	switch {
	case len(pathogenic) > 0 && len(benign) > 0:
		// Discordant directions require review.
		basis = "conflicting"
	case seen["BA1"] || bs >= 2:
		classification = "Benign"
	case (bs >= 1 && bp >= 1) || bp >= 2:
		classification = "Likely_Benign"
	case (vs >= 1 && (s >= 1 || m >= 2 || (m >= 1 && p >= 1) || p >= 2)) ||
		s >= 2 || (s >= 1 && (m >= 3 || (m >= 2 && p >= 2) || (m >= 1 && p >= 4))):
		classification = "Pathogenic"
	case (vs >= 1 && m >= 1) || (s >= 1 && (m >= 1 || p >= 2)) ||
		m >= 3 || (m >= 2 && p >= 2) || (m >= 1 && p >= 4):
		classification = "Likely_Pathogenic"
	}
	if classification != "VUS" {
		basis = "combination"
	}
	return &mtACMGAssessment{Profile: "clingen-mtdna-v1", Classification: classification, Basis: basis, Evidence: evidence}, nil
}
