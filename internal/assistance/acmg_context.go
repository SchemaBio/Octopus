package assistance

import (
	"regexp"
	"strings"
)

var privateAnnotationKey = regexp.MustCompile(`(?i)(password|token|secret|api.?key|sample.?id|patient.?name|file.?path|fastq|bam.?path|cram.?path)`)
var canonicalAIGene = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,80}$`)

// Keep all annotation columns, while excluding operational and identity data.
func acmgVariantAnnotations(v map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range strings.Fields("gene genes chromosome position ref alt variantType zygosity transcript hgvsc hgvsp consequence clinvarId clinvarSignificance clinvarReviewStatus clinvarStars clinvarDisease gnomadAF gnomadEasAF alphaMissenseScore pangolinGain pangolinLoss evoScore diseaseAssociation inheritanceMode rsId depth alleleFrequency maxAF gnomadNhomaltXX gnomadNhomaltXY pangolinAnnotation evoAnnotation alphaMissenseClass pubmedIds omimId") {
		if value, ok := v[key]; ok {
			out[key] = value
		}
	}
	columns := map[string]any{}
	if values, ok := v["annotationValues"].(map[string]any); ok {
		for key, value := range values {
			if !privateAnnotationKey.MatchString(key) {
				columns[key] = value
			}
		}
	}
	if values, ok := v["annotationValues"].(map[string]string); ok {
		for key, value := range values {
			if !privateAnnotationKey.MatchString(key) {
				columns[key] = value
			}
		}
	}
	out["annotationValues"] = columns
	return out
}
