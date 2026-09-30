package service

import (
	"context"
	"errors"
	"testing"
)

type fakeCVMReferenceStorage struct {
	sizes map[string]int64
	errs  map[string]error
	seen  []string
}

func (f *fakeCVMReferenceStorage) stat(_ context.Context, key string) (int64, error) {
	f.seen = append(f.seen, key)
	if err := f.errs[key]; err != nil {
		return 0, err
	}
	return f.sizes[key], nil
}

func TestCVMReferenceObjectKeys(t *testing.T) {
	tests := []struct {
		genome string
		bed    bool
		want   []string
	}{
		{"hg19", true, []string{"database/hg19/Homo_sapiens.GRCh37.dna.primary_assembly.fa", "database/hg19/Homo_sapiens.GRCh37.dna.primary_assembly.fa.fai", "database/hg19/hg19_default.bed"}},
		{"hg38", false, []string{"database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa", "database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa.fai"}},
	}
	for _, tc := range tests {
		got, err := cvmReferenceObjectKeys(tc.genome, tc.bed)
		if err != nil {
			t.Fatalf("cvmReferenceObjectKeys(%q): %v", tc.genome, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("keys for %s = %#v, want %#v", tc.genome, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("keys for %s = %#v, want %#v", tc.genome, got, tc.want)
			}
		}
	}
}

func TestCVMReferencePreflightRejectsMissingEmptyAndStorageErrors(t *testing.T) {
	base := map[string]int64{
		"database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa":     100,
		"database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa.fai": 10,
		"database/hg38/hg38_default.bed":                                20,
	}
	for _, tc := range []struct {
		name string
		edit func(*fakeCVMReferenceStorage)
	}{
		{"missing BED", func(f *fakeCVMReferenceStorage) { delete(f.sizes, "database/hg38/hg38_default.bed") }},
		{"empty index", func(f *fakeCVMReferenceStorage) {
			f.sizes["database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa.fai"] = 0
		}},
		{"HEAD error", func(f *fakeCVMReferenceStorage) {
			f.errs["database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa"] = errors.New("provider response")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage := &fakeCVMReferenceStorage{sizes: map[string]int64{}, errs: map[string]error{}}
			for key, size := range base {
				storage.sizes[key] = size
			}
			tc.edit(storage)
			err := validateCVMReferenceObjects(context.Background(), storage, "hg38", true)
			var objectErr *CVMInputObjectError
			if !errors.As(err, &objectErr) || objectErr.ReasonCode != "REFERENCE_DATABASE_FAILED" {
				t.Fatalf("preflight error = %v, want platform reference failure", err)
			}
		})
	}
}

func TestCVMReferencePreflightPassesWithoutDefaultBEDWhenNotUsed(t *testing.T) {
	storage := &fakeCVMReferenceStorage{sizes: map[string]int64{
		"database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa":     100,
		"database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa.fai": 10,
	}, errs: map[string]error{}}
	if err := validateCVMReferenceObjects(context.Background(), storage, "hg38", false); err != nil {
		t.Fatalf("reference preflight without a default BED: %v", err)
	}
}
