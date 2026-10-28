package assistance

import (
	"math"
	"regexp"
	"strings"
)

var privateAnnotationKey = regexp.MustCompile(`(?i)(password|token|secret|api.?key|sample.?id|patient.?name|file.?path|fastq|bam.?path|cram.?path)`)
var canonicalAIGene = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,80}$`)

// Keep all annotation columns, while excluding operational and identity data.
func acmgVariantAnnotations(v map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range strings.Fields("gene genes chromosome position startPosition endPosition coordinateSystem type copyNumber copyRatio confidence exon ref alt variantType zygosity transcript hgvsc hgvsp consequence clinvarId clinvarSignificance clinvarReviewStatus clinvarStars clinvarDisease gnomadAF gnomadEasAF alphaMissenseScore pangolinGain pangolinLoss evoScore diseaseAssociation inheritanceMode rsId depth alleleFrequency maxAF gnomadNhomaltXX gnomadNhomaltXY pangolinAnnotation evoAnnotation alphaMissenseClass pubmedIds omimId") {
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
	if calls := acmgSampleGenotypes(v["sampleGenotypes"]); len(calls) > 0 {
		out["sampleGenotypes"] = calls
	}
	return out
}

// Role labels already follow the dataset's verified sample-column mapping.
// Calls remain caller-supplied context, never validated segregation evidence.
func acmgSampleGenotypes(value any) map[string]any {
	out := map[string]any{}
	roles, _ := value.(map[string]any)
	for _, role := range []string{"proband", "father", "mother"} {
		sample, ok := roles[role].(map[string]any)
		if !ok {
			continue
		}
		call := map[string]any{}
		if gt := text(sample["genotype"]); len(gt) <= 40 && regexp.MustCompile(`^[0-9.]+(?:[/|][0-9.]+)*$`).MatchString(gt) {
			call["genotype"] = gt
		}
		if ad := text(sample["alleleDepth"]); len(ad) <= 128 && regexp.MustCompile(`^[0-9]+(?:, *[0-9]+)*$`).MatchString(ad) {
			call["alleleDepth"] = ad
		}
		if v, ok := sample["alleleFrequency"].(float64); ok && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 {
			call["alleleFrequency"] = v
		}
		for _, field := range []string{"depth", "genotypeQuality"} {
			if n, ok := sample[field].(float64); ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 9007199254740991 && n == math.Trunc(n) {
				call[field] = n
			}
		}
		switch z := text(sample["zygosity"]); z {
		case "Reference", "Heterozygous", "Homozygous", "Hemizygous", "Unknown":
			call["zygosity"] = z
		}
		switch source := text(sample["vafSource"]); source {
		case "reported", "ad":
			call["vafSource"] = source
		}
		if len(call) > 0 {
			out[role] = call
		}
	}
	return out
}
