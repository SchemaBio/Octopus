package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestStageCustomCNVReferenceRewritesUnsignedBaselineURL(t *testing.T) {
	orgID := "30229951-d4e7-4dd9-8791-3d751e422a06"
	baselineID := "00cae3ad-ffb2-4235-9ba4-60444c0572b0"
	objectKey := "organizations/" + orgID + "/baselines/" + baselineID + "/reference.cnn"
	host := "jakarta-user-1327430028.cos.ap-jakarta.myqcloud.com"
	storage := &fakeCVMInputStorage{sizes: map[string]int64{objectKey: 4096}}
	inputs := map[string]interface{}{
		"SingleWES.cnvkit_reference": "https://" + host + "/" + objectKey,
		"SingleWES.bed":              "/mnt/data/inputs/panel.bed",
	}
	downloads := []model.CVMInputDownload{{
		ObjectKey: "organizations/" + orgID + "/uploads/panel.bed",
		Target:    "/mnt/data/inputs/panel.bed", ValidationRole: "bed", ExpectedSizeBytes: 12,
	}}

	staged, err := stageCustomCNVReferences(context.Background(), storage, orgID, host, inputs, downloads, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	wantTarget := "/mnt/data/inputs/" + baselineID + "-reference.cnn"
	if inputs["SingleWES.cnvkit_reference"] != wantTarget {
		t.Fatalf("reference path = %v", inputs["SingleWES.cnvkit_reference"])
	}
	if len(staged) != 2 || staged[1].ObjectKey != objectKey || staged[1].Target != wantTarget || staged[1].ValidationRole != "cnv_reference" || staged[1].ExpectedSizeBytes != 4096 || staged[1].PairKey != "" {
		t.Fatalf("staged download = %#v", staged)
	}
	if storage.statCalls != 1 || storage.presignCalls != 1 {
		t.Fatalf("HEAD/presign = %d/%d", storage.statCalls, storage.presignCalls)
	}
	if strings.Contains(inputs["SingleWES.cnvkit_reference"].(string), "https://") {
		t.Fatal("MiniWDL input still contains a remote URL")
	}
}

func TestStageCustomCNVReferenceKeepsPlatformDatabaseFile(t *testing.T) {
	storage := &fakeCVMInputStorage{}
	inputs := map[string]interface{}{
		"SingleWES.cnvkit_reference": "/mnt/data/database/schema_bundle/hg19_reference.cnvkit.cnn",
	}
	downloads, err := stageCustomCNVReferences(context.Background(), storage, "30229951-d4e7-4dd9-8791-3d751e422a06", "bucket.cos.ap-jakarta.myqcloud.com", inputs, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 0 || storage.statCalls != 0 || storage.presignCalls != 0 {
		t.Fatalf("platform reference was staged: %#v calls=%d/%d", downloads, storage.statCalls, storage.presignCalls)
	}
	if inputs["SingleWES.cnvkit_reference"] != "/mnt/data/database/schema_bundle/hg19_reference.cnvkit.cnn" {
		t.Fatalf("platform path changed: %v", inputs["SingleWES.cnvkit_reference"])
	}
}

func TestStageCustomCNVReferenceRejectsForeignOrMissingObjects(t *testing.T) {
	orgID := "30229951-d4e7-4dd9-8791-3d751e422a06"
	host := "jakarta-user-1327430028.cos.ap-jakarta.myqcloud.com"
	foreign := "https://" + host + "/organizations/11111111-1111-4111-8111-111111111111/baselines/00cae3ad-ffb2-4235-9ba4-60444c0572b0/reference.cnn"
	missingKey := "organizations/" + orgID + "/baselines/00cae3ad-ffb2-4235-9ba4-60444c0572b0/reference.cnn"
	storage := &fakeCVMInputStorage{statErrors: map[string]error{missingKey: errors.New("missing")}}
	for _, value := range []string{
		foreign,
		"https://other.example/" + missingKey,
		"https://" + host + "/" + missingKey,
		"/mnt/data/inputs/reference.cnn",
	} {
		inputs := map[string]interface{}{"SingleWES.cnvkit_reference": value}
		_, err := stageCustomCNVReferences(context.Background(), storage, orgID, host, inputs, nil, time.Hour)
		if err == nil {
			t.Fatalf("accepted %s", value)
		}
		if strings.Contains(err.Error(), "https://") || strings.Contains(err.Error(), orgID) {
			t.Fatalf("error leaked locator: %v", err)
		}
	}
	if storage.presignCalls != 0 {
		t.Fatalf("presigned %d URLs after a failed baseline check", storage.presignCalls)
	}
}

func TestBuildCVMWDLInputsAppliesCustomBaselineToTrio(t *testing.T) {
	inputs, err := buildCVMWDLInputs("trio", "hg19", map[string]interface{}{
		"bed_file":     "/mnt/data/inputs/panel.bed",
		"cnv_baseline": "https://bucket.example/reference.cnn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inputs["TrioWES.cnvkit_reference"] != "https://bucket.example/reference.cnn" || inputs["TrioWES.bed"] != "/mnt/data/inputs/panel.bed" {
		t.Fatalf("trio baseline was not applied: %#v", inputs["TrioWES.cnvkit_reference"])
	}
	without, err := buildCVMWDLInputs("trio", "hg19", map[string]interface{}{"bed_file": "/mnt/data/database/hg19_default.bed"})
	if err != nil {
		t.Fatal(err)
	}
	if without["TrioWES.cnvkit_reference"] != "/mnt/data/database/schema_bundle/hg19_reference.cnvkit.cnn" {
		t.Fatalf("default trio reference changed: %v", without["TrioWES.cnvkit_reference"])
	}
}
