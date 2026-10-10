package service

import (
	"context"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestSVCv4SelectedProjectionPreservesLegacyEvidence(t *testing.T) {
	legacy := map[string]interface{}{"activeAcmgVersion": "svcv4", "acmgOverride": "Benign", "acmgClassification": "Benign",
		"svcv4Assessment": map[string]interface{}{"confirmed": true, "revision": SVCv4Revision, "result": map[string]interface{}{"classification": "VUS", "vusSubclass": "VUS-high", "state": "classified", "score": 4.0}}}
	if err := validateActiveACMG(legacy); err != nil {
		t.Fatal(err)
	}
	row := normalizeParquetAPIItem("snv-indel", map[string]interface{}{"__adjustments": legacy, "__acmg": map[string]interface{}{"classification": "Likely_Benign"}})
	if row["acmgClassification"] != "VUS" || row["acmgVusSubclass"] != "VUS-high" || legacy["acmgClassification"] != "Benign" {
		t.Fatalf("lost independent versions: %#v", row)
	}
	classification, err := historyEffectiveClassification(nil, &model.ResultDataset{Table: "snv-indel"}, "row", legacy)
	if err != nil || classification != "VUS" {
		t.Fatalf("history ignored selected version: %v %s", err, classification)
	}
	legacy["activeAcmgVersion"] = "legacy"
	classification, err = historyEffectiveClassification(nil, &model.ResultDataset{Table: "snv-indel"}, "row", legacy)
	if err != nil || classification != "Benign" {
		t.Fatal("legacy result lost")
	}
}

func TestSVCv4AdoptionRequiresConfirmedPinnedClassification(t *testing.T) {
	for _, assessment := range []map[string]interface{}{
		{}, {"confirmed": true, "revision": SVCv4Revision, "result": map[string]interface{}{"state": "insufficient_evidence"}},
		{"confirmed": false, "revision": SVCv4Revision, "result": map[string]interface{}{"state": "classified", "classification": "VUS"}},
		{"confirmed": true, "revision": "different", "result": map[string]interface{}{"state": "classified", "classification": "VUS"}},
	} {
		if validateActiveACMG(map[string]interface{}{"activeAcmgVersion": "svcv4", "svcv4Assessment": assessment}) == nil {
			t.Fatal("invalid adoption accepted")
		}
	}
	if validateActiveACMG(map[string]interface{}{}) != nil {
		t.Fatal("old record not compatible")
	}
}

func TestSVCv4GoChecksReferenceRevision(t *testing.T) {
	for _, revision := range []string{SVCv4Revision, "other"} {
		cfg := &config.Config{}
		result, err := (&ResultService{cfg: cfg}).SVCv4(context.Background(), map[string]interface{}{"revision": revision, "disease": "Synthetic", "moi": "AD", "inputs": map[string]interface{}{}})
		if (err == nil) != (revision == SVCv4Revision) {
			t.Fatalf("version check failed: %v", err)
		}
		if err == nil && (result["revision"] != SVCv4Revision || result["authoritative"] != false) {
			t.Fatal("lost reference provenance")
		}
	}
}

func TestReportContractRequiresVersionProvenance(t *testing.T) {
	raw := `{"reported_variants":[{"interpretation":{"activeAcmgVersion":"svcv4"}}]}`
	if validateReportACMGContract(raw, "legacy-v1") == nil {
		t.Fatal("legacy report silently loses selected version")
	}
	if err := validateReportACMGContract(raw, "report-snapshot-v2"); err != nil {
		t.Fatal(err)
	}
	if err := validateReportACMGContract(`{"reported_variants":[]}`, "legacy-v1"); err != nil {
		t.Fatal(err)
	}
}
