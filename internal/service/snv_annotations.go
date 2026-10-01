package service

import "strings"

var snvAnnotationColumns = []string{
	"Gene", "Transcript", "Location", "Consequence", "Impact", "HGVS_c", "HGVS_p", "Amino_Acids", "Cytoband",
	"ClinVar_Sig", "ClinVar_RevStat", "ClinVar_DN", "ClinVar_Star",
	"GnomAD_AF", "GnomAD_AF_EAS", "GnomAD_nhomalt_XX", "GnomAD_nhomalt_XY",
	"Pangolin_Gain", "Pangolin_Loss", "Pangolin_AN", "EVOScore", "EVOScore_AN", "AlphaMissense_AM", "AlphaMissense_AMC",
	"HGNC_ID", "dbSNP", "MAX_AF", "GenCC_moi_curie", "GenCC_disease_title", "GenCC_moi_title", "GenCC_disease_original_curie", "GenCC_assertion_criteria_url",
}

func annotationText(value string) string {
	value = strings.TrimSpace(value)
	if value == "." {
		return ""
	}
	return value
}

// Preserve the actual report values, including multi-valued scores joined by
// '&'. The typed numeric fields remain compatible projections, not replacements
// for these source annotations. Prediction labels do not imply ACMG assessment.
func snvAnnotationValues(row map[string]string) map[string]string {
	values := make(map[string]string)
	for _, column := range snvAnnotationColumns {
		if value := annotationText(row[column]); value != "" {
			values[column] = value
		}
	}
	return values
}
