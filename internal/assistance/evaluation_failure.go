package assistance

// Preserve known execution failures without returning a local baseline or
// arbitrary upstream text as a usable AI result.
func evaluationFailureCode(raw map[string]any) string {
	if text(raw["delivery_status"]) != "failed" {
		return ""
	}
	switch code := text(raw["failure_code"]); code {
	case "AI_OUTPUT_INVALID", "AI_PROVIDER_UNAVAILABLE", "AI_TOOL_BUDGET_EXCEEDED":
		return code
	default:
		return "invalid_result"
	}
}
