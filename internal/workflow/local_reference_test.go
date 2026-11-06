package workflow

import (
	"path/filepath"
	"testing"
)

func TestUseLocalReferenceDir(t *testing.T) {
	referenceDir := t.TempDir()
	inputs, err := InputsForGenome("single", "hg38")
	if err != nil {
		t.Fatal(err)
	}
	customFASTA := "/mnt/data/database/custom.fa"
	inputs["SingleWES.fasta"] = customFASTA
	exampleRead := inputs["SingleWES.read_1"]

	UseLocalReferenceDir(inputs, "single", "hg38", referenceDir)
	if got, want := inputs["SingleWES.bed"], filepath.Join(referenceDir, "hg38_default.bed"); got != want {
		t.Errorf("BED = %v, want %v", got, want)
	}
	if got := inputs["SingleWES.ref_dir"]; got != referenceDir {
		t.Errorf("reference directory = %v, want %v", got, referenceDir)
	}
	if inputs["SingleWES.fasta"] != customFASTA || inputs["SingleWES.read_1"] != exampleRead {
		t.Errorf("custom or example input changed: %v", inputs)
	}
	cloudInputs, err := InputsForGenome("single", "hg38")
	if err != nil {
		t.Fatal(err)
	}
	if got := cloudInputs["SingleWES.bed"]; got != "/mnt/data/database/hg38_default.bed" {
		t.Errorf("cloud BED = %v", got)
	}
}
