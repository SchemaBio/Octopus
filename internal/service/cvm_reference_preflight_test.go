package service

import (
	"context"
	"errors"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
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

func TestBaselineReferencePreflightIncludesMappingIndexesAndReference(t *testing.T) {
	for _, genome := range []string{"hg19", "hg38"} {
		keys, err := cvmBaselineReferenceObjectKeys(genome, false)
		if err != nil || len(keys) != 5 {
			t.Fatalf("baseline resource keys=%v error=%v", keys, err)
		}
		storage := &fakeCVMReferenceStorage{sizes: map[string]int64{}, errs: map[string]error{}}
		for _, key := range keys {
			storage.sizes[key] = 10
		}
		if err := validateCVMBaselineReferences(context.Background(), storage, genome, false); err != nil {
			t.Fatal(err)
		}
		delete(storage.sizes, keys[len(keys)-1])
		if err := validateCVMBaselineReferences(context.Background(), storage, genome, false); err == nil {
			t.Fatal("missing built-in baseline accepted")
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

func TestBaselinePreflightPrefersMinibwaAndRequiresCompleteFallback(t *testing.T) {
	for _, genome := range []string{"hg19", "hg38"} {
		keys, _ := cvmBaselineReferenceObjectKeys(genome, false)
		for _, mapper := range []string{"minibwa", "bwa", "incomplete", "empty"} {
			t.Run(genome+"/"+mapper, func(t *testing.T) {
				storage := &fakeCVMReferenceStorage{sizes: map[string]int64{}}
				for _, key := range keys {
					storage.sizes[key] = 10
				}
				if mapper != "minibwa" {
					delete(storage.sizes, keys[0]+".mbw")
				}
				if mapper == "bwa" || mapper == "incomplete" {
					for _, suffix := range []string{".amb", ".ann", ".bwt", ".pac", ".sa"} {
						storage.sizes[keys[0]+suffix] = 20
					}
					if mapper == "incomplete" {
						delete(storage.sizes, keys[0]+".sa")
					}
				}
				if mapper == "empty" {
					storage.sizes[keys[0]+".mbw"] = 0
				}
				err := validateCVMBaselineReferences(context.Background(), storage, genome, false)
				if mapper == "incomplete" || mapper == "empty" {
					var objectErr *CVMInputObjectError
					if !errors.As(err, &objectErr) || objectErr.ReasonCode != "REFERENCE_DATABASE_FAILED" {
						t.Fatalf("incomplete mapping indexes accepted: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if mapper == "minibwa" {
					for _, key := range storage.seen {
						if key == keys[0]+".amb" {
							t.Fatal("standard BWA must not be checked when minibwa is ready")
						}
					}
				}
			})
		}
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

func TestCVMReferenceStorageConfigUsesDedicatedReadCredentials(t *testing.T) {
	base := config.StorageConfig{S3AccessKey: "tenant-key", S3SecretKey: "tenant-secret", S3SessionToken: "tenant-session",
		CVMReferenceAccessKey: "reference-key", CVMReferenceSecretKey: "reference-secret"}
	got, err := cvmReferenceStorageConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.S3AccessKey != "reference-key" || got.S3SecretKey != "reference-secret" || got.S3SessionToken != "" {
		t.Fatal("reference preflight must use only the dedicated read credentials")
	}
	if base.S3AccessKey != "tenant-key" || base.S3SessionToken != "tenant-session" {
		t.Fatal("reference credential selection must not mutate shared storage configuration")
	}
}

func TestCVMReferenceStorageConfigRejectsIncompleteCredentials(t *testing.T) {
	_, err := cvmReferenceStorageConfig(config.StorageConfig{CVMReferenceAccessKey: "reference-key"})
	if err == nil {
		t.Fatal("expected incomplete reference credentials to fail")
	}
}
