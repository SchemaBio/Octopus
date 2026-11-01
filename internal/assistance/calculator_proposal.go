package assistance

// A typed proposal is distinct from reviewer-confirmed calculator inputs.
// Only paths supported by the corresponding calculator are exposed.
func calculatorProposal(feature string, ai map[string]any) *CalculatorDraft {
	if feature != "acmg" && feature != "svcv4" {
		return nil
	}
	draft := &CalculatorDraft{Version: feature, Changes: []CalculatorChange{}}
	if feature == "acmg" {
		items, _ := ai["criteria"].([]any)
		for _, value := range items {
			item, ok := value.(map[string]any)
			if !ok {
				continue
			}
			code := text(item["code"])
			if code == "" {
				continue
			}
			reason := text(item["reason"])
			if reason == "" {
				reason = text(item["override_reason"])
			}
			draft.Changes = append(draft.Changes, CalculatorChange{Path: "acmgEvidence." + code, Value: map[string]any{"code": code, "strength": item["strength"]}, Status: text(item["status"]), Reason: reason, SourceIDs: stringArray(item["source_ids"])})
		}
	} else if fields, ok := ai["svc_context"].(map[string]any); ok {
		for key, path := range map[string]string{"disease": "disease", "moi": "moi", "geneDiseaseValidity": "geneDiseaseValidity", "workflow": "inputs.workflow"} {
			field, _ := fields[key].(map[string]any)
			if value := text(field["value"]); value != "" {
				draft.Changes = append(draft.Changes, CalculatorChange{Path: path, Value: value, Status: "pending_review", Reason: text(field["reason"]), SourceIDs: stringArray(field["source_ids"])})
			}
		}
	}
	return draft
}

func stringArray(raw any) []string {
	var out []string
	if values, ok := raw.([]any); ok {
		for _, value := range values {
			if s, ok := value.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
