package svcv4

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPartialSVCContextNeverInventsDiseaseInheritanceOrClassification(t *testing.T) {
	for _, context := range []object{{}, {"disease": "D"}, {"moi": "AD"}} {
		input := object{"disease": context["disease"], "moi": context["moi"], "confirmed": true, "inputs": object{"population": object{"faf": 0.0, "daft": 0.001, "hmz_eligible": "TRUE", "homozygote_count": 5.0}}}
		result, err := EvaluatePartial(input)
		if err != nil {
			t.Fatal(err)
		}
		r := obj(result["result"])
		if r["state"] != "insufficient_context" || r["score"] != nil || r["classification"] != nil || result["confirmed"] != false {
			t.Fatal("incomplete context yielded a final result")
		}
		if len(r["missingFields"].([]string)) == 0 {
			t.Fatal("missing fields not explained")
		}
		pop := r["details"].(object)["population"].(*ScoreResult)
		if n, ok := pop.Sub.Values["POP_FRQ"]; !ok || n != 0 {
			t.Fatal("real zero frequency score lost")
		}
		if _, ok := pop.Sub.Values["POP_HMZ"]; ok {
			t.Fatal("unknown context scored inherited homozygote evidence")
		}
	}
	empty, err := EvaluatePartial(object{})
	if err != nil || obj(empty["result"])["score"] != nil {
		t.Fatal("blank draft must yield an unscored assessment", err)
	}
}
func TestPartialSVCStillRejectsInvalidInputs(t *testing.T) {
	for _, input := range []object{
		{"moi": "invented"},
		{"inputs": object{"population": object{"faf": 2.0}}},
		{"inputs": object{"invented": true}},
		{"inputs": object{"workflow": "unsupported"}},
	} {
		if _, err := EvaluatePartial(input); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}
func TestPartialSVCAdapterPreservesCompleteReferenceResults(t *testing.T) {
	raw, err := os.ReadFile("testdata/evaluate.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []oracleCase
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		var input object
		_ = json.Unmarshal(fixture.Input, &input)
		if fixture.Error != nil || str(input["disease"]) == "" || str(input["moi"]) == "" {
			continue
		}
		want, err := Evaluate(input)
		if err != nil {
			t.Fatal(err)
		}
		got, err := EvaluatePartial(input)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(got)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("complete reference calculation changed")
		}
	}
}
