package service

import "testing"

func TestSupportsParquetObjectStorage(t *testing.T) {
	for _, provider := range []string{"cos", "COS", "s3", " S3 "} {
		if !supportsParquetObjectStorage(provider) {
			t.Errorf("provider %q should support Parquet archive queries", provider)
		}
	}
	for _, provider := range []string{"", "local", "filesystem"} {
		if supportsParquetObjectStorage(provider) {
			t.Errorf("provider %q should not be treated as object storage", provider)
		}
	}
}

func TestParquetTableMatchUsesWorkflowOutputNames(t *testing.T) {
	got := map[string]string{
		"73ac.snv_indel.parquet":      "snv-indel",
		"73ac.region.cnvanno.parquet": "cnv-segment",
		"73ac.gene.cnvanno.parquet":   "cnv-exon",
		"73ac.str.parquet":            "str",
		"73ac.mei.parquet":            "mei",
		"73ac.mt_report.parquet":      "mt",
		"73ac.roh.anno.parquet":       "roh",
	}
	for filename, table := range got {
		if !parquetTableMatch(table, filename) {
			t.Errorf("workflow output %q was not recognized as table %q", filename, table)
		}
	}
	if parquetTableMatch("cnv-segment", "73ac.gene.cnvanno.parquet") {
		t.Fatal("gene-level CNV file must not match the segment table")
	}
}
