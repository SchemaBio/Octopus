package assistance

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

const importedFactsVersion = "imported-facts-v1"

// Imported facts are traceable context, never independently verified evidence.
func importedFactPack(body map[string]any) map[string]any {
	facts := []map[string]any{}
	add := func(field, path string, applicable bool) {
		value := normalizeFact(field, factValue(body, path))
		status := "reported"
		if !applicable {
			value = nil
			status = "not_applicable"
		} else if unknownFact(value) {
			value = nil
			status = "unknown"
		}
		facts = append(facts, map[string]any{"field": field, "value": value, "source": "request", "path": path, "version": "unversioned", "status": status})
	}
	_, sequence := body["variant"].(map[string]any)
	_, region := body["cnv"].(map[string]any)
	for _, field := range strings.Fields("assembly chrom pos ref alt gene transcript hgvs_c hgvs_p consequence allele_frequency") {
		add("variant."+field, "/variant/"+field, sequence)
	}
	for _, field := range strings.Fields("assembly chrom start end coordinate_system type copy_number") {
		add("cnv."+field, "/cnv/"+field, region)
	}
	for _, field := range strings.Fields("disease_id inheritance sample_type variant_origin hpo_terms") {
		add("context."+field, "/clinical_context/"+field, true)
	}
	for _, field := range strings.Fields("gene transcript consequence clinvarId clinvarSignificance clinvarReviewStatus clinvarStars clinvarDisease gnomadAF gnomadEasAF maxAF depth alleleFrequency alphaMissenseScore pangolinGain pangolinLoss copyNumber copyNumberEstimate copyRatio confidence") {
		applicable := true
		switch field {
		case "copyNumber", "copyNumberEstimate", "copyRatio":
			applicable = region
		case "alphaMissenseScore", "pangolinGain", "pangolinLoss":
			applicable = sequence
		}
		add("annotation."+field, "/evidence_package/ai_context/variant_annotations/"+field, applicable)
	}
	for _, field := range strings.Fields("analysis_mode gender age sample_type father_affected_status mother_affected_status") {
		add("sample."+field, "/evidence_package/ai_context/sample/"+field, true)
	}
	add("sample.hpo_terms", "/evidence_package/ai_context/sample/hpo_terms", true)
	for _, role := range []string{"proband", "father", "mother"} {
		for _, field := range []string{"genotype", "alleleDepth", "alleleFrequency", "depth", "genotypeQuality", "zygosity", "vafSource"} {
			add("family."+role+"."+field, "/evidence_package/ai_context/variant_annotations/sampleGenotypes/"+role+"/"+field, true)
		}
	}
	for _, field := range []string{"identity_confirmed", "de_novo_confirmed", "phase_confirmed", "segregation_confirmed"} {
		facts = append(facts, map[string]any{"field": "family." + field, "value": nil, "source": "request", "path": "", "version": "unversioned", "status": "unknown"})
	}
	engine, _ := body["engine_id"].(string)
	for _, field := range []string{"hi", "ts", "protein_coding_count", "complete_overlap", "orientation"} {
		applicable := region && ((field != "hi" && field != "ts") || field == "hi" && engine == "cnv_loss_2020" || field == "ts" && engine == "cnv_gain_2020")
		status := "unknown"
		if !applicable {
			status = "not_applicable"
		}
		facts = append(facts, map[string]any{"field": "dosage." + field, "value": nil, "source": "request", "path": "", "version": "unversioned", "status": status})
	}
	// Detect a contradictory transcript/gene assertion without choosing the
	// annotation over the fixed variant. Coordinate conventions are not guessed.
	for _, field := range []string{"gene", "transcript", "consequence"} {
		var identity, annotation map[string]any
		for _, fact := range facts {
			if fact["field"] == "variant."+field {
				identity = fact
			}
			if fact["field"] == "annotation."+field {
				annotation = fact
			}
		}
		if identity != nil && annotation != nil && identity["status"] == "reported" && annotation["status"] == "reported" && identity["value"] != annotation["value"] {
			identity["status"] = "conflicting"
			annotation["status"] = "conflicting"
		}
	}
	return map[string]any{"version": importedFactsVersion, "engine_id": engine, "facts": facts, "reported_is_not_verified": true}
}

func factValue(body map[string]any, path string) any {
	var current any = body
	for _, key := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return current
}
func unknownFact(value any) bool {
	if value == nil {
		return true
	}
	if v, ok := value.(string); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", ".", "unknown", "unavailable", "not_provided":
			return true
		}
	}
	if v, ok := value.(float64); ok {
		return math.IsNaN(v) || math.IsInf(v, 0)
	}
	if v, ok := value.([]any); ok {
		return len(v) == 0
	}
	if v, ok := value.([]string); ok {
		return len(v) == 0
	}
	return false
}

func normalizeFact(field string, value any) any {
	if unknownFact(value) {
		return nil
	}
	numeric := strings.HasSuffix(field, ".pos") || strings.HasSuffix(field, ".start") || strings.HasSuffix(field, ".end") || strings.HasSuffix(field, ".age") || strings.HasSuffix(field, ".copy_number")
	for _, suffix := range []string{".allele_frequency", ".alleleFrequency", ".gnomadAF", ".gnomadEasAF", ".maxAF", ".depth", ".genotypeQuality", ".alphaMissenseScore", ".pangolinGain", ".pangolinLoss", ".copyNumber", ".copyNumberEstimate", ".copyRatio", ".clinvarStars"} {
		numeric = numeric || strings.HasSuffix(field, suffix)
	}
	if numeric {
		n, ok := value.(float64)
		if !ok {
			if text, yes := value.(string); yes {
				var err error
				n, err = strconv.ParseFloat(text, 64)
				ok = err == nil
			}
		}
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 9007199254740991 {
			return nil
		}
		if strings.HasSuffix(field, ".pos") || strings.HasSuffix(field, ".start") || strings.HasSuffix(field, ".end") || strings.HasSuffix(field, ".age") || strings.HasSuffix(field, ".depth") || strings.HasSuffix(field, ".copy_number") || strings.HasSuffix(field, ".clinvarStars") {
			if math.Trunc(n) != n {
				return nil
			}
		}
		if strings.Contains(field, "AF") || strings.HasSuffix(field, ".alleleFrequency") || strings.HasSuffix(field, ".allele_frequency") || strings.HasSuffix(field, ".alphaMissenseScore") {
			if n > 1 {
				return nil
			}
		}
		return n
	}
	if field == "sample.hpo_terms" || field == "context.hpo_terms" {
		ids := []string{}
		seen := map[string]bool{}
		add := func(v any) {
			id, _ := v.(string)
			if object, ok := v.(map[string]any); ok {
				id, _ = object["id"].(string)
			}
			if len(id) != 10 || !strings.HasPrefix(id, "HP:") {
				return
			}
			for _, r := range id[3:] {
				if r < '0' || r > '9' {
					return
				}
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				add(item)
			}
		case []string:
			for _, item := range v {
				add(item)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		return ids
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if len(text) > 512 {
			return nil
		}
		if strings.HasSuffix(field, ".genotype") && strings.Contains(text, ".") {
			return nil
		}
		return text
	}
	return nil
}

// Typed backend sample structures are normalized to their actual wire representation.
func attachImportedFactPack(body, pkg map[string]any) {
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}
	var normalized map[string]any
	if json.Unmarshal(raw, &normalized) != nil {
		return
	}
	if imported, ok := pkg["ai_context"].(map[string]any); ok {
		imported["fact_pack"] = importedFactPack(normalized)
	}
}
