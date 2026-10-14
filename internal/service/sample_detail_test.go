package service

import (
	"reflect"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestSampleDetailUsesSameHPOAsList(t *testing.T) {
	svc := &SampleService{}
	sample := &model.Sample{ClinicalDiagnosis: `{"mainDiagnosis":"Seizure","hpoTerms":[{"id":"HP:0001249","name":"Old term"}]}`}
	terms := []model.HPOTerm{{ID: "HP:0001250", Name: "Seizure"}}
	sample.SetHPOTerms(terms)
	detail := svc.SampleToDetailResponse(sample)
	if detail.ClinicalDiagnosis.MainDiagnosis != "Seizure" || !reflect.DeepEqual(detail.ClinicalDiagnosis.HPOTerms, svc.SampleToResponse(sample).HPOTerms) {
		t.Fatalf("detail and list disagree: %+v", detail.ClinicalDiagnosis)
	}
	sample.SetHPOTerms([]model.HPOTerm{})
	if got := svc.SampleToDetailResponse(sample).ClinicalDiagnosis.HPOTerms; len(got) != 0 {
		t.Fatalf("cleared terms were restored from diagnosis: %+v", got)
	}
}

func TestSampleDetailIncludesHPOWithPlainDiagnosis(t *testing.T) {
	sample := &model.Sample{ClinicalDiagnosis: "Developmental delay"}
	sample.SetHPOTerms([]model.HPOTerm{{ID: "HP:0001263", Name: "Global developmental delay"}})
	got := (&SampleService{}).SampleToDetailResponse(sample)
	if got.ClinicalDiagnosis.MainDiagnosis != sample.ClinicalDiagnosis || len(got.ClinicalDiagnosis.HPOTerms) != 1 || got.ClinicalDiagnosis.HPOTerms[0].ID != "HP:0001263" {
		t.Fatalf("missing clinical information: %+v", got.ClinicalDiagnosis)
	}
}
