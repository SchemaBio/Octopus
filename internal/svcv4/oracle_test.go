package svcv4

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

type oracleCase struct {
	Function string          `json:"function"`
	Input    json.RawMessage `json:"input"`
	Output   json.RawMessage `json:"output"`
	Error    interface{}     `json:"error"`
}

func compareOracle(t *testing.T, got interface{}, want json.RawMessage) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected interface{}
	json.Unmarshal(encoded, &actual)
	json.Unmarshal(want, &expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("oracle mismatch\nactual %s\nexpected %s", encoded, want)
	}
}
func TestEvaluatePythonOracle(t *testing.T) {
	for _, name := range []string{"evaluate", "validation"} {
		t.Run(name, func(t *testing.T) { testEvaluateFile(t, name) })
	}
}
func testEvaluateFile(t *testing.T, name string) {
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []oracleCase
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var input object
			json.Unmarshal(c.Input, &input)
			result, err := Evaluate(input)
			if err == nil {
				_, err = json.Marshal(result)
			}
			if c.Error != nil {
				if err == nil {
					t.Fatal("expected rejection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			compareOracle(t, result, c.Output)
		})
	}
}
func decodeResult(raw json.RawMessage) *ScoreResult {
	var r ScoreResult
	json.Unmarshal(raw, &r)
	return &r
}
func TestUpstreamPythonOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/upstream.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []oracleCase
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%03d_%s", i, c.Function), func(t *testing.T) {
			var a object
			json.Unmarshal(c.Input, &a)
			var raw map[string]json.RawMessage
			json.Unmarshal(c.Input, &raw)
			moi, gdv := str(a["moi"]), str(a["gene_disease_validity"])
			var result interface{}
			var err error
			switch c.Function {
			case "cap":
				result = Cap(value(a["value"]), *value(a["lo"]), *value(a["hi"]))
			case "hold_combined":
				parts := []*float64{}
				for _, v := range list(a["parts"]) {
					parts = append(parts, value(v))
				}
				result = Hold(*value(a["lo"]), *value(a["hi"]), parts...)
			case "informative_points":
				result = informative(list(a["variants"]), false)
			case "missense_informative_points":
				result = informative(list(a["variants"]), true)
			case "apply_sm18_multiplier":
				result = SM18(value(a["points"]), str(a["gencc_mechanism"]), str(a["exon_relevance"]), gdv, a["mechanism_only"] == true)
			case "transcript_relevance_points":
				result = Transcript(value(a["points"]), str(a["exon_relevance"]))
			case "reference_classify":
				category, sub := Classify(*value(a["points"]))
				result = object{"category": category, "vus_subclass": sub}
			case "_doubled_tally":
				s, w := int(*value(a["n_strong"])), int(*value(a["n_weak"]))
				n := 0.0
				if s+w > 0 {
					n = 2
					if s > 0 {
						n = 4
					}
					n += 2 * float64(s+w-1)
				}
				result = n
			case "_standard_tally":
				s, w := int(*value(a["n_strong"])), int(*value(a["n_weak"]))
				n := 0.0
				if s > 0 {
					n += 2
				}
				if w > 0 {
					n++
				}
				n += float64(max(s-1, 0) + max(w-1, 0))
				result = n
			case "reference_aggregate_pop", "reference_aggregate_loc", "reference_aggregate_cln_cases":
				var rows []json.RawMessage
				json.Unmarshal(raw["results"], &rows)
				results := []*ScoreResult{}
				for _, row := range rows {
					results = append(results, decodeResult(row))
				}
				switch c.Function {
				case "reference_aggregate_pop":
					result, err = Aggregate(results, "POP", nil)
				case "reference_aggregate_loc":
					result, err = Aggregate(results, "LOC", num(4))
				default:
					result = AggregateClinical(results)
				}
			case "reference_finalize_cln":
				var ccs *ScoreResult
				if a["ccs"] != nil {
					ccs = decodeResult(raw["ccs"])
				}
				result = FinalizeClinical(decodeResult(raw["cln_subtotal"]), ccs, value(a["pop_frq_points"]))
			case "reference_combine_case":
				var rows []json.RawMessage
				json.Unmarshal(raw["subtotals"], &rows)
				families := []familyTotal{}
				for _, row := range rows {
					var m object
					json.Unmarshal(row, &m)
					if _, ok := m["amino_acid"]; ok {
						var v MissenseResult
						json.Unmarshal(row, &v)
						families = append(families, familyTotal{&v.Code, v.Total})
					} else {
						v := decodeResult(row)
						families = append(families, familyTotal{v.ParentCode, v.Total})
					}
				}
				result, err = Combine(families)
			case "reference_score_population":
				result = Population(obj(a["evidence"]), moi)
			case "reference_score_loc_phe":
				result = LocusPhenotype(obj(a["case"]), moi)
			case "reference_score_loc_seg":
				result = LocusSegregation(obj(a["case"]), moi)
			case "reference_score_cln_uaf":
				result = ClinicalUnaffected(obj(a["case"]), moi)
			case "reference_score_cln_alt":
				result = ClinicalAlternate(obj(a["case"]), moi)
			case "reference_score_cln_aff_mono":
				result = ClinicalMono(obj(a["case"]), moi)
			case "reference_score_cln_aff_biallelic":
				result = ClinicalBiallelic(obj(a["case"]), moi)
			case "reference_score_cln_dnv":
				var bi *bool
				if v, ok := a["is_biallelic"].(bool); ok {
					bi = &v
				}
				result = ClinicalDeNovo(obj(a["case"]), moi, bi)
			case "reference_score_cln_ccs":
				result = CaseControl(obj(a["evidence"]))
			case "reference_score_cln_proband":
				result = ClinicalProband(obj(a["case"]), moi)
			default:
				if strings.HasPrefix(c.Function, "reference_score_") {
					r, m := Impact(strings.TrimPrefix(c.Function, "reference_score_"), obj(a["assessment"]), gdv)
					if m != nil {
						result = m
					} else {
						result = r
					}
				} else {
					t.Fatalf("unhandled function %s", c.Function)
				}
			}
			if c.Error != nil {
				if err == nil {
					t.Fatal("expected rejection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			compareOracle(t, result, c.Output)
		})
	}
}
