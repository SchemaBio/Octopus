package assistance

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImportedFactPackKeepsZeroAndUnknown(t *testing.T) {
	_, body, err := BuildPayload(Input{Feature: "cnv_loss", Reference: "hg38", Variant: map[string]any{"chromosome": "1", "startPosition": float64(0), "endPosition": float64(100), "coordinateSystem": "0-based-half-open", "copyNumber": float64(0), "privatePatientName": "secret"}}, "profile")
	if err != nil {
		t.Fatal(err)
	}
	pack := body["evidence_package"].(map[string]any)["ai_context"].(map[string]any)["fact_pack"].(map[string]any)
	facts := pack["facts"].([]map[string]any)
	for _, fact := range facts {
		if fact["status"] == "verified" {
			t.Fatal("import promoted to verified")
		}
		if fact["field"] == "annotation.copyNumber" && (fact["value"] != float64(0) || fact["status"] != "reported") {
			t.Fatal("zero lost")
		}
		if fact["field"] == "variant.transcript" && fact["status"] != "not_applicable" {
			t.Fatal("missing data invented")
		}
	}
	raw, _ := json.Marshal(pack)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "privatePatientName") {
		t.Fatal("private values copied")
	}
}

func TestImportedFactPackEngineAndFamilyContract(t *testing.T) {
	for _, feature := range []string{"acmg", "svcv4", "cnv_gain", "cnv_loss"} {
		t.Run(feature, func(t *testing.T) {
			variant := map[string]any{"chromosome": "1", "position": float64(100), "ref": "A", "alt": "G", "startPosition": float64(0), "endPosition": float64(100), "coordinateSystem": "0-based-half-open", "copyNumber": float64(1.25), "sampleGenotypes": map[string]any{"proband": map[string]any{"genotype": "1|0", "alleleDepth": "0,30", "depth": float64(30), "genotypeQuality": float64(0)}, "father": map[string]any{"genotype": "./.", "alleleFrequency": float64(0)}}}
			_, body, err := BuildPayload(Input{Feature: feature, Mode: "experimental", Reference: "hg38", Variant: variant, SampleContext: map[string]any{"analysis_mode": "trio", "age": 0, "hpo_terms": []any{map[string]any{"id": "HP:0001250", "name": "synthetic"}}}}, "profile")
			if err != nil {
				t.Fatal(err)
			}
			pack := body["evidence_package"].(map[string]any)["ai_context"].(map[string]any)["fact_pack"].(map[string]any)
			facts := map[string]map[string]any{}
			for _, fact := range pack["facts"].([]map[string]any) {
				facts[fact["field"].(string)] = fact
			}
			if facts["family.father.genotype"]["status"] != "unknown" || facts["family.father.alleleFrequency"]["value"] != float64(0) || facts["family.proband.genotypeQuality"]["value"] != float64(0) || facts["family.de_novo_confirmed"]["status"] != "unknown" || facts["sample.age"]["value"] != float64(0) {
				t.Fatal("zero, missing or confirmation semantics changed")
			}
			if feature == "cnv_gain" && facts["dosage.hi"]["status"] != "not_applicable" {
				t.Fatal("gain borrowed HI")
			}
			if feature == "cnv_loss" && facts["dosage.ts"]["status"] != "not_applicable" {
				t.Fatal("loss borrowed TS")
			}
			if facts["family.proband.genotype"]["path"] != "/evidence_package/ai_context/variant_annotations/sampleGenotypes/proband/genotype" {
				t.Fatal("missing provenance pointer")
			}
		})
	}
}
