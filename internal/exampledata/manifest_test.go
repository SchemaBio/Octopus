package exampledata

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func object(path string, role Role, data string) Object {
	sum := sha256.Sum256([]byte(data))
	return Object{Path: path, Role: role, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func validManifest() Manifest {
	return Manifest{Version: "v1", SampleID: "example", ReferenceGenome: "hg38", Pipeline: "wes", PipelineVersion: "1", Objects: []Object{
		object("data/sample_R1.fastq.gz", Read1, "read1"), object("data/sample_R2.fastq.gz", Read2, "read2"),
		object("result/sample.bam", BAM, "bam"), object("result/sample.bam.bai", BAMIndex, "bai"), object("result/variants.parquet", Result, "result"),
	}}
}

func TestManifestValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Manifest)
	}{
		{"path traversal", func(m *Manifest) { m.Objects[0].Path = "../read1.fastq.gz" }},
		{"absolute path", func(m *Manifest) { m.Objects[0].Path = "/read1.fastq.gz" }},
		{"backslash", func(m *Manifest) { m.Objects[0].Path = `data\read1.fastq.gz` }},
		{"duplicate", func(m *Manifest) { m.Objects = append(m.Objects, m.Objects[0]) }},
		{"missing bam", func(m *Manifest) { m.Objects = append(m.Objects[:2], m.Objects[3:]...) }},
		{"archive", func(m *Manifest) { m.Objects[4].Path = "result/all.zip" }},
		{"bam disguised as result", func(m *Manifest) { m.Objects[4].Path = "result/another.bam" }},
		{"bad checksum", func(m *Manifest) { m.Objects[0].SHA256 = strings.Repeat("x", 64) }},
		{"bad version", func(m *Manifest) { m.Version = "../other" }},
	}
	if err := validManifest().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest()
			tc.change(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestVerifyObject(t *testing.T) {
	o := object("data/r1.fastq.gz", Read1, "correct")
	for _, tc := range []struct {
		input string
		valid bool
	}{{"correct", true}, {"incorrect", false}, {"correc", false}, {"correct-extra", false}} {
		err := VerifyObject(o, strings.NewReader(tc.input))
		if (err == nil) != tc.valid {
			t.Fatalf("input %q: %v", tc.input, err)
		}
	}
}

func TestApprovalDeadlines(t *testing.T) {
	approved := time.Date(2026, 10, 27, 13, 0, 0, 0, time.FixedZone("CST", 8*3600))
	d, err := ApprovalDeadlines(approved)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Data.Equal(approved.Add(168*time.Hour)) || !d.BAM.Equal(d.Data) || !d.Sample.Equal(approved.Add(720*time.Hour)) || !d.Result.Equal(d.Sample) {
		t.Fatalf("wrong deadlines: %+v", d)
	}
	if _, err := ApprovalDeadlines(time.Time{}); err == nil {
		t.Fatal("missing approval time accepted")
	}
}
