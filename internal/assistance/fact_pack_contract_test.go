package assistance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFactPackWireContract(t *testing.T) {
	type contract struct {
		Feature string         `json:"feature"`
		Request map[string]any `json:"request"`
	}
	cases := []contract{}
	for _, feature := range []string{"acmg", "svcv4", "cnv_gain", "cnv_loss"} {
		input := Input{Feature: feature, Mode: "experimental", Reference: "hg38", Variant: map[string]any{"chromosome": "1", "position": float64(100), "ref": "A", "alt": "G", "gene": "SYNTHETIC", "transcript": "ENST_SYNTHETIC", "consequence": "missense_variant", "gnomadAF": float64(0), "startPosition": float64(0), "endPosition": float64(100), "coordinateSystem": "0-based-half-open", "copyNumber": float64(0), "sampleGenotypes": map[string]any{"proband": map[string]any{"genotype": "0/1", "alleleDepth": "20,10", "depth": float64(30), "genotypeQuality": float64(40)}, "father": map[string]any{"genotype": "./.", "alleleFrequency": float64(0)}}}, SampleContext: map[string]any{"analysis_mode": "trio", "age": 0, "gender": "female", "hpo_terms": []any{map[string]any{"id": "HP:0001250"}}}}
		input.IncludeFactPack = true
		_, body, err := BuildPayload(input, "synthetic-profile")
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, contract{Feature: feature, Request: body})
	}
	actual, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	path := filepath.Join("testdata", "fact_pack_contract.json")
	if os.Getenv("UPDATE_AI_FACT_CONTRACT") == "true" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, actual, 0644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Git may normalize line endings on Windows; whitespace is not a wire contract.
	var golden any
	if err := json.Unmarshal(expected, &golden); err != nil {
		t.Fatal(err)
	}
	expected, err = json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	expected = append(expected, '\n')
	if string(actual) != string(expected) {
		t.Fatal("wire contract changed; update and verify the SepiLens receiver fixture together")
	}
}
