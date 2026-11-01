package service

import (
	"context"
	"github.com/SchemaBio/Octopus/internal/model"
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
