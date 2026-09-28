package service

import (
	"strings"
	"testing"
	"time"
)

func TestIGVCandidatesUseOnlyManifestDeclaredFilesAndIndexes(t *testing.T) {
	objects := map[string]s3ObjectInfo{
		"proband.bam":         {Key: "prefix/proband.bam"},
		"proband.bai":         {Key: "prefix/proband.bai"},
		"father.bam":          {Key: "prefix/father.bam"},
		"variants.vcf.gz":     {Key: "prefix/variants.vcf.gz"},
		"variants.vcf.gz.tbi": {Key: "prefix/variants.vcf.gz.tbi"},
		"targets.bed":         {Key: "prefix/targets.bed"},
		"unrelated.bam":       {Key: "prefix/unrelated.bam"},
	}
	manifest := map[string]interface{}{
		"summary": map[string]interface{}{
			"bam":         []interface{}{"/work/proband.bam", "/work/father.bam"},
			"bai":         []interface{}{"/work/proband.bai"},
			"members":     []interface{}{"proband", "father"},
			"vcf_raw":     "/work/variants.vcf.gz",
			"vcf_raw_tbi": "/work/variants.vcf.gz.tbi",
			"bed":         "/work/targets.bed",
		},
	}

	candidates := igvCandidatesFromManifest(manifest, objects)
	if len(candidates) != 4 {
		t.Fatalf("got %d candidates, want four manifest-declared tracks", len(candidates))
	}
	if candidates[0].objectKey != "prefix/proband.bam" || !candidates[0].descriptor.HasIndex || candidates[0].descriptor.MemberRole != "proband" {
		t.Fatalf("unexpected proband BAM candidate: %#v", candidates[0])
	}
	if candidates[1].objectKey != "prefix/father.bam" || candidates[1].descriptor.Available || candidates[1].descriptor.Reason != "BAM 索引缺失" {
		t.Fatalf("father BAM should be retained but unavailable without an index: %#v", candidates[1])
	}
	for _, candidate := range candidates {
		if candidate.objectKey == "prefix/unrelated.bam" {
			t.Fatal("archive object absent from manifest became an IGV track")
		}
	}
}

func TestIGVArchiveObjectIndexRejectsAmbiguousBasenames(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	manifest, objects, version, err := igvArchiveObjectIndex("prefix", []s3ObjectInfo{
		{Key: "prefix/outputs.resolved.json", Size: 10, LastModified: now},
		{Key: "prefix/a/reads.bam", Size: 20, LastModified: now},
		{Key: "prefix/b/reads.bam", Size: 30, LastModified: now},
	})
	if err != nil {
		t.Fatalf("igvArchiveObjectIndex() error = %v", err)
	}
	if manifest != "prefix/outputs.resolved.json" || version == "" {
		t.Fatalf("unexpected manifest index: manifest=%q version=%q", manifest, version)
	}
	if _, found := objects["reads.bam"]; found {
		t.Fatal("ambiguous archive basename remained addressable")
	}
}

func TestArchiveReferenceBaseNameDoesNotRetainTraversal(t *testing.T) {
	for _, input := range []string{"../reads.bam", `C:\\work\\reads.bam`, "https://example.invalid/a/reads.bam?token=secret"} {
		base := archiveReferenceBaseName(input)
		if strings.Contains(base, "/") || strings.Contains(base, `\\`) || base != "reads.bam" {
			t.Fatalf("archiveReferenceBaseName(%q) = %q", input, base)
		}
	}
}
