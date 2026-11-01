package service

import (
	"context"
	"encoding/json"
	"github.com/SchemaBio/Octopus/internal/assistance"
	"github.com/SchemaBio/Octopus/internal/model"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAISequenceRequiresImmutableRowIdentityBeforeReading(t *testing.T) {
	for _, input := range []map[string]interface{}{{}, {"rowOrdinal": -1.0}, {"rowOrdinal": 1.5}, {"rowOrdinal": "0"}} {
		if _, err := (&ResultService{}).AISequenceVariant(context.Background(), &model.Task{}, input); err == nil {
			t.Fatal("invalid browser row identity accepted")
		}
	}
}
func TestAISampleCallsKeepVAFSeparateFromPopulationFrequency(t *testing.T) {
	calls := aiSampleCalls(map[string]interface{}{"Genotype": "0/1", "AD": "23,22", "VAF": ".", "GnomAD_AF": .0000009, "Depth": 45})
	proband := calls["proband"].(map[string]interface{})
	if proband["alleleFrequency"] != 22.0/45.0 || proband["vafSource"] != "ad" || calls["father"] != nil {
		t.Fatal(calls)
	}
}

func TestAISequenceParquetStringPositionReachesPayloadAsExactCoordinate(t *testing.T) {
	input := filepath.Join(t.TempDir(), "public.snv_indel.txt")
	if err := os.WriteFile(input, []byte("Chromosome\tPosition\tRef\tAlt\tGene\tTranscript\tHGVS_c\tHGVS_p\tConsequence\n17\t10544634\tC\tT\tMYH3\tNM_002470.4\tc.2015G>A\tp.Arg672His\tmissense variant\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, count, _, err := convertArchivedTextParquet(input, "11111111-1111-4111-8111-111111111111")
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	defer os.Remove(file)
	page, err := NewParquetReader().ReadPage(file, 0, 1)
	if err != nil || len(page.Rows) != 1 {
		t.Fatal(err)
	}
	raw := page.Rows[0]
	if raw["Position"] != "10544634" {
		t.Fatal("fixture must exercise archived UTF-8 coordinates")
	}

	d := &model.ResultDataset{DataVersion: "fixed", ObjectSHA256: "fixed-sha"}
	row, err := canonicalAISequenceRow(raw, d, "attempt", "row", 0)
	if err != nil {
		t.Fatal(err)
	}
	contracts := map[string]interface{}{}
	for _, feature := range []string{"acmg", "svcv4"} {
		_, payload, err := assistance.BuildPayload(assistance.Input{Feature: feature, Mode: "experimental", Reference: "hg19", Variant: row}, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		contracts[feature] = payload
		v := payload["variant"].(map[string]interface{})
		if v["pos"] != float64(10544634) || v["consequence"] != "missense_variant" || v["hgvs_c"] != "c.2015G>A" {
			t.Fatalf("typed variant identity lost: %#v", v)
		}
	}
	encoded, err := json.MarshalIndent(contracts, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contractFile := filepath.Join("testdata", "ai-sequence-contract.json")
	if os.Getenv("UPDATE_AI_SEQUENCE_CONTRACT") == "true" {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(contractFile, append(encoded, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(contractFile)
	if err != nil {
		t.Fatal(err)
	}
	var want, got interface{}
	if json.Unmarshal(expected, &want) != nil || json.Unmarshal(encoded, &got) != nil || !reflect.DeepEqual(want, got) {
		t.Fatal("AI sequence wire contract changed")
	}
	annotations := row["annotationValues"].(map[string]interface{})
	if annotations["Position"] != "10544634" {
		t.Fatal("raw coordinate lost")
	}
}

func TestAISequenceRejectsInvalidArchivedCoordinatesBeforeSubmission(t *testing.T) {
	for _, position := range []interface{}{nil, "", ".", "-1", "1.5", "NaN", "Inf", "9007199254740993"} {
		if _, err := canonicalAISequenceRow(map[string]interface{}{"Position": position}, &model.ResultDataset{}, "attempt", "row", 0); err == nil {
			t.Fatalf("invalid coordinate accepted: %v", position)
		}
	}
	for _, position := range []interface{}{int64(10544634), float64(10544634)} {
		row, err := canonicalAISequenceRow(map[string]interface{}{"Position": position}, &model.ResultDataset{}, "attempt", "row", 0)
		if err != nil || row["position"] != float64(10544634) {
			t.Fatal("numeric Parquet coordinate changed", err)
		}
	}
}
