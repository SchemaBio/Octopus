package assistance

import (
	"encoding/json"
	"math"
	"testing"
)

func TestCNVAutomaticContextAndCoordinates(t *testing.T) {
	for _, feature := range []string{"cnv_gain", "cnv_loss"} {
		in := Input{Feature: feature, Reference: "hg38", SampleContext: map[string]any{"analysis_mode": "trio"}, Variant: map[string]any{"chromosome": "1", "startPosition": float64(0), "endPosition": float64(100), "coordinateSystem": "0-based-half-open", "copyNumber": float64(0)}}
		_, p, e := BuildPayload(in, "p")
		if e != nil {
			t.Fatal(e)
		}
		c := p["cnv"].(map[string]any)
		if c["start"] != float64(1) || c["end"] != float64(100) || c["copy_number"] != 0 || p["variant"] != nil {
			t.Fatal(p)
		}
		if p["evidence_package"].(map[string]any)["ai_context"] == nil {
			t.Fatal("missing context")
		}
		for _, bad := range []any{"", "unknown"} {
			in.Variant["coordinateSystem"] = bad
			if _, _, e = BuildPayload(in, "p"); e == nil {
				t.Fatal("unknown coordinates accepted")
			}
		}
		in.Variant["coordinateSystem"] = "1-based-inclusive"
		if _, _, e = BuildPayload(in, "p"); e == nil {
			t.Fatal("zero one-based position")
		}
		in.Variant["startPosition"] = float64(1)
		in.Variant["copyNumber"] = float64(.5)
		_, p, e = BuildPayload(in, "p")
		if e != nil {
			t.Fatal("valid depth-derived estimate rejected", e)
		}
		if _, exists := p["cnv"].(map[string]any)["copy_number"]; exists {
			t.Fatal("fractional CN rounded into discrete count")
		}
		annotations := p["evidence_package"].(map[string]any)["ai_context"].(map[string]any)["variant_annotations"].(map[string]any)
		if annotations["copyNumber"] != float64(.5) || annotations["copyNumberEstimate"] != float64(.5) {
			t.Fatal("estimate lost")
		}
		in.Variant["copyNumber"] = float64(-.5)
		if _, _, e = BuildPayload(in, "p"); e == nil {
			t.Fatal("negative CN accepted")
		}
	}
}
func TestCNVEmptyAndFailedPaidDelivery(t *testing.T) {
	for _, feature := range []string{"cnv_gain", "cnv_loss"} {
		raw := map[string]any{"schema_version": "sepilens-evaluation-2.0", "delivery_status": "delivered", "result_kind": "evidence_only", "settlement_eligible": true, "agent_proposal": map[string]any{"ai_assessment": map[string]any{"clinical_summary": "synthetic", "criteria": []any{map[string]any{"code": "2A", "status": "insufficient_data"}}}}}
		_, bill, e := normalizeResult(feature, "mock", raw, true)
		if e != nil || !bill {
			t.Fatal(bill, e)
		}
		raw["delivery_status"] = "failed"
		if _, _, e = normalizeResult(feature, "mock", raw, true); e == nil {
			t.Fatal("failed baseline delivered")
		}
		_, _ = json.Marshal(raw)
	}
}

func TestCNVCopyNumberKinds(t *testing.T) {
	for _, feature := range []string{"cnv_gain", "cnv_loss"} {
		for _, test := range []struct {
			value     float64
			valid     bool
			estimated bool
		}{{0, true, false}, {1, true, false}, {3, true, false}, {1.73, true, true}, {2.61, true, true}, {-1, false, false}, {math.NaN(), false, false}, {math.Inf(1), false, false}, {9007199254740992, false, false}} {
			in := Input{Feature: feature, Reference: "hg19", Variant: map[string]any{"chromosome": "1", "startPosition": float64(0), "endPosition": float64(100), "coordinateSystem": "0-based-half-open", "copyNumber": test.value}}
			_, p, e := BuildPayload(in, "p")
			if (e == nil) != test.valid {
				t.Fatalf("%s value=%v error=%v", feature, test.value, e)
			}
			if !test.valid {
				continue
			}
			cnv := p["cnv"].(map[string]any)
			ann := p["evidence_package"].(map[string]any)["ai_context"].(map[string]any)["variant_annotations"].(map[string]any)
			if test.estimated {
				if _, ok := cnv["copy_number"]; ok || ann["copyNumberEstimate"] != test.value {
					t.Fatal("estimate rounded or lost")
				}
			} else if cnv["copy_number"] != int(test.value) {
				t.Fatal("integer count changed")
			}
		}
	}
}
