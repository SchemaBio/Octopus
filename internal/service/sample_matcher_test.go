package service

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestParseFASTQPairName(t *testing.T) {
	tests := []struct {
		name, key string
		read      model.ReadType
	}{
		{"SAMPLE-01_R1.fastq.gz", "SAMPLE-01", model.ReadTypeRead1},
		{"SAMPLE-01_S3_L001_R2_001.fastq.gz", "SAMPLE-01", model.ReadTypeRead2},
		{"case.2.fq", "case", model.ReadTypeRead2},
	}
	for _, tt := range tests {
		key, read, ok := parseFASTQPairName(tt.name)
		if !ok || key != tt.key || read != tt.read {
			t.Fatalf("parse %q = %q/%q/%v", tt.name, key, read, ok)
		}
	}
	if _, _, ok := parseFASTQPairName("notes.txt"); ok {
		t.Fatal("non-FASTQ file matched")
	}
}
