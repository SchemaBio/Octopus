package assistance

import "testing"

func TestEvaluationFailurePreservesKnownCauseWithoutDeliveringBaseline(t *testing.T) {
	for _, code := range []string{"AI_OUTPUT_INVALID", "AI_PROVIDER_UNAVAILABLE", "AI_TOOL_BUDGET_EXCEEDED"} {
		raw := map[string]any{"delivery_status": "failed", "failure_code": code, "local_baseline": map[string]any{"classification": "Pathogenic"}}
		if evaluationFailureCode(raw) != code {
			t.Fatal(code)
		}
		if _, _, err := NormalizeResult("acmg", "synthetic", raw); err == nil {
			t.Fatal("failed AI delivered local baseline")
		}
	}
	if evaluationFailureCode(map[string]any{"delivery_status": "failed", "failure_code": "arbitrary private message"}) != "invalid_result" {
		t.Fatal("unknown upstream text leaked")
	}
	if evaluationFailureCode(map[string]any{"delivery_status": "delivered", "failure_code": "AI_OUTPUT_INVALID"}) != "" {
		t.Fatal("non-failed result misclassified")
	}
}
