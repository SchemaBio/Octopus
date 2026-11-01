package assistance

import "strings"

// Normalize equivalent annotation spellings, not biological consequences. Do
// not infer missense from an HGVS protein string or borrow another transcript.
func canonicalConsequence(value string) string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool { return r == '&' || r == ',' || r == ';' || r == '|' })
	for i, part := range parts {
		part = strings.Join(strings.Fields(part), "_")
		switch part {
		case "missense", "synonymous", "frameshift", "inframe_insertion", "inframe_deletion":
			part += "_variant"
		}
		parts[i] = part
	}
	return strings.Join(parts, "&")
}
