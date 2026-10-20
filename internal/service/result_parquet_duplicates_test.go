package service

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestUniqueArchivedParquetObject(t *testing.T) {
	for _, tc := range []struct {
		name, left, right string
		size              int64
		wantError         bool
	}{
		{"identical", "PAR1same", "PAR1same", 8, false}, {"conflicting", "PAR1left", "PAR1diff", 8, true}, {"truncated", "short", "short", 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"a.parquet": tc.left, "b.parquet": tc.right}
			chosen, err := uniqueArchivedParquetObject([]s3ObjectInfo{{Key: "b.parquet", Size: tc.size}, {Key: "a.parquet", Size: tc.size}}, func(key string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(files[key])), nil })
			if (err != nil) != tc.wantError {
				t.Fatalf("chosen=%v err=%v", chosen, err)
			}
			if !tc.wantError && chosen.Key != "a.parquet" {
				t.Fatalf("unstable choice: %s", chosen.Key)
			}
		})
	}
}
func TestUniqueArchivedParquetObjectReadFailure(t *testing.T) {
	_, err := uniqueArchivedParquetObject([]s3ObjectInfo{{Key: "a", Size: 1}, {Key: "b", Size: 1}}, func(string) (io.ReadCloser, error) { return nil, errors.New("unavailable") })
	if err == nil {
		t.Fatal("read failure was ignored")
	}
}

func TestVCFDownloadArchiveNamesPairIndexes(t *testing.T) {
	id := "de82e3d3-b149-4b4b-84ef-99cc3712516f"
	for _, kind := range []string{"normalized", "annotated"} {
		vcf, err := vcfDownloadArchiveName(id, kind+"_vcf")
		if err != nil {
			t.Fatal(err)
		}
		index, err := vcfDownloadArchiveName(id, kind+"_vcf_tbi")
		if err != nil || index != vcf+".tbi" {
			t.Fatalf("vcf=%s index=%s err=%v", vcf, index, err)
		}
	}
	if _, err := vcfDownloadArchiveName(id, "bam"); err == nil {
		t.Fatal("VCF download accepted BAM")
	}
	if _, err := vcfDownloadArchiveName("../../escape", "summary"); err == nil {
		t.Fatal("unsafe task id accepted")
	}
}
