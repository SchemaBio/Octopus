package assistance

import (
	"math"
	"testing"
)

func TestAISequencePreservesMissenseAndDoesNotConfusePopulationAFWithVAF(t *testing.T) {
	in := Input{Feature: "acmg", Reference: "hg19", Variant: map[string]any{"gene": "MYH3", "chromosome": "17", "position": float64(10544634), "ref": "C", "alt": "T", "transcript": "NM_002470.4", "hgvsc": "c.2015G>A", "hgvsp": "p.Arg672His", "consequence": "missense variant", "alleleFrequency": .49, "gnomadAF": .0000009}}
	_, body, err := BuildPayload(in, "profile")
	if err != nil {
		t.Fatal(err)
	}
	v := body["variant"].(map[string]any)
	if v["consequence"] != "missense_variant" || v["transcript"] != "NM_002470.4" || v["hgvs_c"] != "c.2015G>A" || v["hgvs_p"] != "p.Arg672His" {
		t.Fatal(v)
	}
	pkg := body["evidence_package"].(map[string]any)
	if annotations, ok := pkg["annotations"].(map[string]any); ok && annotations["gnomad"] != nil {
		t.Fatal("sample VAF or global AF was promoted to popmax")
	}
}

func TestAISequenceRejectsUnnormalizedOrInvalidPositionBeforeQueuing(t *testing.T) {
	for _, position := range []interface{}{nil, "10544634", float64(0), float64(-1), 1.5, math.NaN(), math.Inf(1), float64(9007199254740992)} {
		_, _, err := BuildPayload(Input{Feature: "acmg", Reference: "hg19", Variant: map[string]interface{}{"chromosome": "17", "position": position}}, "synthetic")
		if err == nil {
			t.Fatal("invalid sequence position could be queued")
		}
	}
}
