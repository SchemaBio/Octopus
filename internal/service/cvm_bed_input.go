package service

import (
	"fmt"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
)

// Catalog examples are not input assets. In particular, the CVM image does not
// contain the catalog's /mnt/data/test capture intervals.
func cvmAnalysisBEDInput(template string, inputs map[string]interface{}) (string, error) {
	key := map[string]string{
		"single": "SingleWES.bed", "trio": "TrioWES.bed", "baseline_fix": "CNVBaselineFix.bed",
	}[template]
	if key == "" {
		return "", nil
	}
	value, _ := inputs[key].(string)
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/mnt/data/test/") {
		return "", fmt.Errorf("CVM analysis requires the reference database default BED or an uploaded BED data asset")
	}
	return value, nil
}

// The genome's default BED is downloaded with the reference database. Custom
// BED files must be backed by a tenant asset staged onto this instance.
func validateCVMStagedBED(template string, inputs map[string]interface{}, downloads []model.CVMInputDownload) error {
	bed, err := cvmAnalysisBEDInput(template, inputs)
	if err != nil || bed == "" {
		return err
	}
	genome, err := cvmReferenceGenome(inputs)
	if err != nil {
		return err
	}
	if bed == "/mnt/data/database/"+genome+"_default.bed" {
		return nil
	}
	for _, download := range downloads {
		if download.Target == bed {
			return nil
		}
	}
	return fmt.Errorf("CVM BED input is not backed by a staged data asset; select a pipeline with an uploaded BED file")
}
