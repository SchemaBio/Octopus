package service

import "testing"

func TestReplaceInputStringRecurses(t *testing.T) {
	inputs := map[string]interface{}{
		"direct": "objects/r1.fastq.gz",
		"nested": []interface{}{
			map[string]interface{}{"file": "objects/r1.fastq.gz"},
			"unchanged",
		},
	}
	replaced := replaceInputString(inputs, "objects/r1.fastq.gz", "/data/inputs/r1.fastq.gz").(map[string]interface{})
	if replaced["direct"] != "/data/inputs/r1.fastq.gz" {
		t.Fatalf("direct input was not replaced: %#v", replaced)
	}
	nested := replaced["nested"].([]interface{})
	if nested[0].(map[string]interface{})["file"] != "/data/inputs/r1.fastq.gz" || nested[1] != "unchanged" {
		t.Fatalf("nested input replacement is wrong: %#v", nested)
	}
}

func TestSafeCVMInputName(t *testing.T) {
	if got := safeCVMInputName(`../sample R1;rm.fastq.gz`); got != "sample_R1_rm.fastq.gz" {
		t.Fatalf("unexpected safe input name: %q", got)
	}
}

func TestStructuredResultFileSelection(t *testing.T) {
	for _, name := range []string{"sample.snv_indel.tsv", "sample.region.cnvanno.txt", "sample.str.txt", "sample.roh.tsv"} {
		if !isStructuredResultFile(name) {
			t.Fatalf("expected structured result file: %s", name)
		}
	}
	for _, name := range []string{"sample.bam", "sample.vcf.gz", "sample.snv_indel.parquet", "workflow.log"} {
		if isStructuredResultFile(name) {
			t.Fatalf("unexpected structured result file: %s", name)
		}
	}
}
