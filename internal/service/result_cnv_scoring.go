package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

func verifyCNVEventType(original, assessment map[string]interface{}) error {
	declared := ""
	for _, key := range []string{"Type", "CNV_Type", "Col4", "type"} {
		if value, ok := original[key].(string); ok && strings.TrimSpace(value) != "" && value != "." {
			declared = strings.ToUpper(strings.TrimSpace(value))
			break
		}
	}
	expected := ""
	switch declared {
	case "DEL", "DELETION", "LOSS":
		expected = "Deletion"
	case "DUP", "DUPLICATION", "GAIN", "AMPLIFICATION":
		expected = "Amplification"
	}
	if expected == "" || assessment["cnvType"] != expected {
		return fmt.Errorf("CNV calculator event does not match immutable source")
	}
	return nil
}

// Recompute manual calculator submissions; a browser's total/classification is
// never a financial or clinical source of truth. Evidence prerequisites remain
// the user's recorded responsibility for manual selections.
func normalizeCNVScoring(payload map[string]interface{}) error {
	data, _ := json.Marshal(payload)
	var p struct {
		CNVType  string                     `json:"cnvType"`
		Criteria map[string]json.RawMessage `json:"criteria"`
	}
	if json.Unmarshal(data, &p) != nil || (p.CNVType != "Deletion" && p.CNVType != "Amplification") {
		return fmt.Errorf("unknown CNV event type")
	}
	sections := map[string]float64{}
	evidence := false
	var one struct {
		Selected string `json:"selected"`
	}
	if json.Unmarshal(p.Criteria["section1"], &one) != nil {
		return fmt.Errorf("missing section 1")
	}
	switch one.Selected {
	case "":
	case "1A":
		evidence = true
	case "1B":
		sections["section1"] = -.6
		evidence = true
	default:
		return fmt.Errorf("unknown section 1 option")
	}
	var two map[string]struct {
		Selected string  `json:"selected"`
		Score    float64 `json:"score"`
	}
	if json.Unmarshal(p.Criteria["section2"], &two) != nil {
		return fmt.Errorf("missing section 2")
	}
	type bounds struct{ min, max, def float64 }
	loss := map[string]bounds{"2A": {1, 1, 1}, "2B": {0, 0, 0}, "2C-1": {.45, 1, .9}, "2C-2": {0, .45, 0}, "2D-1": {0, 0, 0}, "2D-2": {.45, .9, .9}, "2D-3": {0, .45, .3}, "2D-4": {.45, 1, .9}, "2E": {0, .9, 0}, "2F": {-1, -1, -1}, "2G": {0, 0, 0}, "2H": {.15, .15, .15}}
	gain := map[string]bounds{"2A": {1, 1, 1}, "2B": {0, 0, 0}, "2C": {-1, -1, -1}, "2D": {-1, -1, -1}, "2E": {0, 0, 0}, "2F": {-1, 0, -.9}, "2G": {0, 0, 0}, "2H": {0, 0, 0}, "2I": {0, .9, 0}, "2J": {0, 0, 0}, "2K": {.45, .45, .45}, "2L": {0, 0, 0}}
	options := loss
	if p.CNVType == "Amplification" {
		options = gain
	}
	selected := 0
	for key, v := range two {
		if v.Selected == "" {
			continue
		}
		selected++
		b, ok := options[v.Selected]
		if !ok {
			return fmt.Errorf("unknown dosage option")
		}
		groups := map[string][]string{"hiOverlap": {"2A", "2B", "2C-1", "2C-2", "2D-1", "2D-2", "2D-3", "2D-4", "2E"}, "benignOverlap": {"2F", "2G"}, "hiPredictor": {"2H"}}
		if p.CNVType == "Amplification" {
			groups = map[string][]string{"tsOverlap": {"2A", "2B"}, "benignOverlap": {"2C", "2D", "2E", "2F", "2G"}, "hiOverlap": {"2H", "2I", "2J", "2K", "2L"}}
		}
		matched := false
		for _, option := range groups[key] {
			if option == v.Selected {
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("invalid dosage group")
		}
		value := v.Score
		if value == 0 && b.def != 0 {
			value = b.def
		}
		if !finiteRange(value, b.min, b.max) {
			return fmt.Errorf("dosage score outside official bounds")
		}
		sections["section2"] += value
		evidence = true
	}
	if selected > 1 {
		return fmt.Errorf("conflicting dosage options must be resolved")
	}
	var three struct {
		Confirmed bool `json:"confirmed"`
		GeneCount int  `json:"geneCount"`
	}
	if json.Unmarshal(p.Criteria["section3"], &three) != nil || three.GeneCount < 0 {
		return fmt.Errorf("invalid coding gene count")
	}
	if three.Confirmed {
		evidence = true
		if p.CNVType == "Deletion" {
			if three.GeneCount >= 35 {
				sections["section3"] = .9
			} else if three.GeneCount >= 25 {
				sections["section3"] = .45
			}
		} else {
			if three.GeneCount >= 50 {
				sections["section3"] = .9
			} else if three.GeneCount >= 35 {
				sections["section3"] = .45
			}
		}
	}
	var four struct {
		DeNovo map[string]struct {
			ConfirmedCount int     `json:"confirmedCount"`
			AssumedCount   int     `json:"assumedCount"`
			Score          float64 `json:"score"`
		} `json:"deNovo"`
		UnknownInheritance map[string]struct {
			Count int `json:"count"`
		} `json:"unknownInheritance"`
		Segregation    map[string]int `json:"segregation"`
		NonSegregation map[string]struct {
			Score float64 `json:"score"`
		} `json:"nonSegregation"`
		CaseControl map[string]struct {
			Score float64 `json:"score"`
		} `json:"caseControl"`
	}
	if json.Unmarshal(p.Criteria["section4"], &four) != nil {
		return fmt.Errorf("missing section 4")
	}
	dn := 0.0
	for i, key := range []string{"4A", "4B", "4C"} {
		v := four.DeNovo[key]
		if v.ConfirmedCount < 0 || v.AssumedCount < 0 || v.ConfirmedCount > 10000 || v.AssumedCount > 10000 {
			return fmt.Errorf("invalid case count")
		}
		dn += float64(v.ConfirmedCount)*[]float64{.45, .30, .15}[i] + float64(v.AssumedCount)*[]float64{.30, .15, .10}[i]
	}
	if !finiteRange(four.DeNovo["4D"].Score, -.3, 0) {
		return fmt.Errorf("invalid 4D score")
	}
	dn += four.DeNovo["4D"].Score
	sections["section4"] = math.Min(dn, .9)
	count := four.UnknownInheritance["4E"].Count
	if count < 0 || count > 10000 {
		return fmt.Errorf("invalid unknown inheritance count")
	}
	sections["section4"] += math.Min(float64(count)*.1, .3)
	seg := 0.0
	for i, key := range []string{"4F", "4G", "4H"} {
		n := four.Segregation[key]
		if n < 0 || n > 10000 {
			return fmt.Errorf("invalid segregation count")
		}
		seg += float64(n) * []float64{.15, .30, .45}[i]
	}
	sections["section4"] += math.Min(seg, .45)
	for _, item := range []struct {
		key      string
		min, max float64
	}{{"4I", -.9, 0}, {"4J", -.9, 0}, {"4K", -.3, 0}, {"4L", 0, .45}, {"4M", 0, .45}, {"4N", -.9, 0}, {"4O", -1, 0}} {
		score := four.NonSegregation[item.key].Score
		if item.key >= "4L" {
			score = four.CaseControl[item.key].Score
		}
		if !finiteRange(score, item.min, item.max) {
			return fmt.Errorf("case score outside official bounds")
		}
		sections["section4"] += score
	}
	if sections["section4"] != 0 {
		evidence = true
	}
	var five map[string]struct {
		Selected string  `json:"selected"`
		Score    float64 `json:"score"`
	}
	if json.Unmarshal(p.Criteria["section5"], &five) != nil {
		return fmt.Errorf("missing section 5")
	}
	ranges := map[string]bounds{"5A": {-.3, .45, 0}, "5B": {-.45, 0, -.3}, "5C": {-.3, 0, -.15}, "5D": {0, .45, 0}, "5E": {-.45, 0, 0}, "5F": {0, 0, 0}, "5G": {0, .15, .1}, "5H": {0, .3, .15}}
	selected = 0
	for key, v := range five {
		if v.Selected == "" {
			continue
		}
		selected++
		validGroup := map[string]string{"5A": "deNovo", "5B": "inherited", "5C": "inherited", "5D": "inherited", "5E": "nonSegregation", "5F": "other", "5G": "other", "5H": "other"}
		if validGroup[v.Selected] != key {
			return fmt.Errorf("invalid inheritance group")
		}
		b, ok := ranges[v.Selected]
		if !ok || !finiteRange(v.Score, b.min, b.max) {
			return fmt.Errorf("inheritance score outside official bounds")
		}
		sections["section5"] += v.Score
		evidence = true
	}
	if selected > 1 {
		return fmt.Errorf("conflicting inheritance options")
	}
	total := 0.0
	for _, key := range []string{"section1", "section2", "section3", "section4", "section5"} {
		total += sections[key]
		if _, ok := sections[key]; !ok {
			sections[key] = 0
		}
	}
	total = math.Round(total*1e8) / 1e8
	classification := ""
	if evidence {
		switch {
		case total >= .99:
			classification = "Pathogenic"
		case total >= .9:
			classification = "Likely_Pathogenic"
		case total <= -.99:
			classification = "Benign"
		case total <= -.9:
			classification = "Likely_Benign"
		default:
			classification = "VUS"
		}
	}
	payload["sectionScores"] = sections
	payload["totalScore"] = total
	payload["classification"] = classification
	payload["assessmentState"] = "evaluated"
	if !evidence {
		payload["assessmentState"] = "insufficient_evidence"
	}
	return nil
}
func finiteRange(n, min, max float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= min && n <= max
}
