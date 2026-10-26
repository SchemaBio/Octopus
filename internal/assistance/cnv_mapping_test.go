package assistance

import (
	"encoding/json"
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
		if _, _, e = BuildPayload(in, "p"); e == nil {
			t.Fatal("fractional absolute CN accepted")
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
