package service

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestCVMAcceptsReferenceDatabaseBED(t *testing.T) {
	for _, template := range []string{"single", "trio", "baseline_fix"} {
		for _, genome := range []string{"hg19", "hg38"} {
			inputs, err := buildCVMWDLInputs(template, genome, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateCVMStagedBED(template, inputs, nil); err != nil {
				t.Fatalf("%s/%s rejected the reference database BED: %v", template, genome, err)
			}
		}
	}
}

func TestCVMRequiresBEDInDownloadPlan(t *testing.T) {
	bed := "/mnt/data/inputs/asset-panel.bed"
	inputs, err := buildCVMWDLInputs("single", "hg19", map[string]interface{}{"bed_file": bed})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCVMStagedBED("single", inputs, []model.CVMInputDownload{{Target: "/mnt/data/inputs/read1.fq.gz"}}); err == nil {
		t.Fatal("a local BED path without a download was accepted")
	}
	if err := validateCVMStagedBED("single", inputs, []model.CVMInputDownload{{Target: bed}}); err != nil {
		t.Fatal(err)
	}
}
