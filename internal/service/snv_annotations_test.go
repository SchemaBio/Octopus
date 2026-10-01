package service

import (
	"encoding/json"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestSNVSourceAnnotationsPreserveLabelsAndMultiValues(t *testing.T) {
	row := map[string]string{"Chromosome": "1", "Position": "123", "Ref": "A", "Alt": "G", "Gene": "GENE", "Transcript": "NM_TEST", "Pangolin_AN": "High likelihood", "EVOScore_AN": "Pathogenic", "AlphaMissense_AM": "0.9&0.7", "GnomAD_AF": "0", "ClinVar_Sig": "Pathogenic", "MAX_AF": ".", "read_sequence": "must-not-be-stored"}
	variant := model.SNVIndel{ID: "stable-id", Chromosome: "1", Position: 123, Ref: "A", Alt: "G", Gene: "GENE", Transcript: "NM_TEST", ACMGClassification: model.ACMGBenign}
	updates, err := buildSNVAnnotationUpdates([]map[string]string{row}, []model.SNVIndel{variant})
	if err != nil {
		t.Fatal(err)
	}
	if updates[0].ID != "stable-id" || updates[0].PangolinAN != "High likelihood" || updates[0].EVOScoreAN != "Pathogenic" {
		t.Fatal("labels or stable identity lost")
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(updates[0].AnnotationValues), &values); err != nil {
		t.Fatal(err)
	}
	if values["AlphaMissense_AM"] != "0.9&0.7" || values["GnomAD_AF"] != "0" {
		t.Fatal("source values changed")
	}
	if _, ok := values["MAX_AF"]; ok {
		t.Fatal("missing value treated as provided")
	}
	if _, ok := values["read_sequence"]; ok {
		t.Fatal("unrecognized data imported")
	}
	if variant.ACMGClassification != model.ACMGBenign {
		t.Fatal("existing assessment changed")
	}
}

func TestSNVAnnotationRecoveryRejectsAmbiguousOrUnmatchedRows(t *testing.T) {
	v := model.SNVIndel{ID: "one", Chromosome: "1", Position: 123, Ref: "A", Alt: "G", Gene: "G", Transcript: "T"}
	row := map[string]string{"Chromosome": "1", "Position": "123", "Ref": "A", "Alt": "G", "Gene": "G", "Transcript": "T"}
	for _, tc := range []struct {
		rows     []map[string]string
		variants []model.SNVIndel
	}{
		{[]map[string]string{row}, nil},
		{[]map[string]string{row, row}, []model.SNVIndel{v, v}},
		{[]map[string]string{{"Chromosome": "2", "Position": "123"}}, []model.SNVIndel{v}},
	} {
		if _, err := buildSNVAnnotationUpdates(tc.rows, tc.variants); err == nil {
			t.Fatal("ambiguous recovery accepted")
		}
	}
}
