package service

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestSampleExpiryClearsOnlyCopiedMemberContent(t *testing.T) {
	independent := model.PedigreeMember{Name: "independent-member", Gender: model.GenderFemale, SampleID: "sample", SourceSampleFields: "[]"}
	updates := expiredSampleMemberUpdates(&independent)
	if _, exists := updates["name"]; exists {
		t.Fatal("independent member erased")
	}
	if _, exists := updates["gender"]; exists {
		t.Fatal("independent gender erased")
	}
	copied := independent
	copied.SourceSampleFields = `["name","gender","phenotypes","birth_year"]`
	updates = expiredSampleMemberUpdates(&copied)
	if updates["name"] != "—" || updates["gender"] != model.GenderUnknown || updates["phenotypes"] != nil || updates["birth_year"] != nil || updates["sample_id"] != "" {
		t.Fatal("source-derived content retained")
	}
	if independent.SampleID != "sample" {
		t.Fatal("inspection mutated source")
	}
}
