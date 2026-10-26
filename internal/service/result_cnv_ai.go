package service

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed cnv_ai_rubrics.json
var cnvAIRubrics []byte

func reviewedCNVSection4(raw json.RawMessage, kind string) (float64, bool, error) {
	var v struct {
		Items []struct {
			Code    string   `json:"code"`
			Score   int64    `json:"scoreHundredths"`
			Sources []string `json:"sourceIds"`
			Reason  string   `json:"reason"`
		} `json:"aiReviewed"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return 0, false, fmt.Errorf("invalid reviewed evidence")
	}
	if len(v.Items) == 0 {
		return 0, false, nil
	}
	var manual map[string]interface{}
	_ = json.Unmarshal(raw, &manual)
	delete(manual, "aiReviewed")
	var nonzero func(interface{}) bool
	nonzero = func(x interface{}) bool {
		switch a := x.(type) {
		case float64:
			return a != 0
		case map[string]interface{}:
			for _, v := range a {
				if nonzero(v) {
					return true
				}
			}
		}
		return false
	}
	if nonzero(manual) {
		return 0, false, fmt.Errorf("reviewed literature cannot duplicate manual case counts")
	}
	var rubrics map[string][]struct {
		Code   string `json:"code"`
		Min    int64  `json:"min"`
		Max    int64  `json:"max"`
		CapMin int64  `json:"cap_min"`
		CapMax int64  `json:"cap_max"`
	}
	_ = json.Unmarshal(cnvAIRubrics, &rubrics)
	sums := map[string]int64{}
	used := map[string]bool{}
	for _, c := range v.Items {
		found := false
		key := "loss"
		if kind == "Amplification" {
			key = "gain"
		}
		for _, d := range rubrics[key] {
			if d.Code != c.Code || c.Code[0] != '4' {
				continue
			}
			found = true
			if _, exists := sums[c.Code]; exists {
				return 0, false, fmt.Errorf("duplicate reviewed clause")
			}
			if c.Score < d.Min || c.Score > d.Max || len(c.Sources) == 0 || c.Reason == "" {
				return 0, false, fmt.Errorf("invalid reviewed clause")
			}
			for _, id := range c.Sources {
				if id == "" || used[id] {
					return 0, false, fmt.Errorf("unresolved duplicate case source")
				}
				used[id] = true
			}
			sums[c.Code] = max(d.CapMin, min(d.CapMax, c.Score))
		}
		if !found {
			return 0, false, fmt.Errorf("unknown reviewed clause")
		}
	}
	sum := func(keys ...string) int64 {
		var n int64
		for _, k := range keys {
			n += sums[k]
		}
		return n
	}
	n := max(int64(-30), min(int64(90), sum("4A", "4B", "4C", "4D"))) + min(int64(45), sum("4F", "4G", "4H")) + min(int64(45), sum("4L", "4M")) + sum("4E", "4I", "4J", "4K", "4N", "4O")
	return float64(n) / 100, true, nil
}
