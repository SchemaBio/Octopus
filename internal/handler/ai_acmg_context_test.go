package handler

import (
	"encoding/json"
	"github.com/SchemaBio/Octopus/internal/model"
	"strings"
	"testing"
)

func TestACMGSampleContextUsesClinicalAllowlist(t *testing.T) {
	age := 12
	sample := &model.Sample{UUID: "private-uuid", InternalID: "private-sample", Gender: model.SampleGenderFemale, Age: &age, SampleType: model.SampleTypeWholeBlood, Batch: "private-batch", Remark: "private-remark", MatchedPair: `{"r1Path":"secret-file"}`, ClinicalDiagnosis: `{"mainDiagnosis":"合成表型 private-sample","symptoms":["合成症状"],"hpoTerms":[{"id":"HP:0001250","name":"Seizure"}],"onsetAge":"2","diseaseHistory":"合成研究背景"}`, FamilyHistory: `{"hasHistory":true,"affectedMembers":[{"relation":"father","condition":"合成家族表型"}]}`}
	context := acmgSampleContext(sample)
	raw, _ := json.Marshal(context)
	for _, excluded := range []string{"private-uuid", "private-sample", "private-batch", "private-remark", "secret-file", "matched_pair"} {
		if strings.Contains(string(raw), excluded) {
			t.Fatal("identity or file metadata included", excluded)
		}
	}
	for _, expected := range []string{"合成表型", "合成研究背景", "HP:0001250", "合成家族表型"} {
		if !strings.Contains(string(raw), expected) {
			t.Fatal("clinical context missing", expected)
		}
	}
}
