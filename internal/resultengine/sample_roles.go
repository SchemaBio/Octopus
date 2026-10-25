package resultengine

import "fmt"

var sampleFields = []string{"Genotype", "Zygosity", "PhaseSet", "Depth", "AD", "VAF", "GQ", "PL"}

// Role mapping is bound to a source object by the archive catalogue. It is
// applied to a copy of each row and leaves ordinal-based row identity intact.
func ValidateSampleColumnRoles(roles map[string]string) error {
	if len(roles) == 0 {
		return nil
	}
	if len(roles) != 3 {
		return fmt.Errorf("invalid sample role mapping")
	}
	seen := map[string]bool{}
	for _, role := range []string{"proband", "father", "mother"} {
		source, ok := roles[role]
		if !ok || (source != "" && source != "father" && source != "mother") || seen[source] {
			return fmt.Errorf("invalid sample role mapping")
		}
		seen[source] = true
	}
	return nil
}
func RemapSampleColumns(row map[string]interface{}, roles map[string]string) {
	if len(roles) == 0 {
		return
	}
	original := make(map[string]interface{}, len(row))
	for k, v := range row {
		original[k] = v
	}
	for _, role := range []string{"proband", "father", "mother"} {
		destination := ""
		if role != "proband" {
			destination = "_" + role
		}
		source := ""
		if roles[role] != "" {
			source = "_" + roles[role]
		}
		for _, field := range sampleFields {
			value, exists := original[field+source]
			if exists {
				row[field+destination] = value
			} else {
				delete(row, field+destination)
			}
		}
	}
}
