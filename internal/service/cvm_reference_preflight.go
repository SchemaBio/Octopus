package service

import (
	"context"
	"fmt"
	"path"
	"strings"
)

const defaultCVMReferenceBucket = "schemabio-1327430028"

type cvmReferenceObjectStorage interface {
	stat(context.Context, string) (int64, error)
}

func cvmReferenceObjectKeys(genome string, requireDefaultBED bool) ([]string, error) {
	var fasta string
	switch strings.ToLower(strings.TrimSpace(genome)) {
	case "hg19":
		fasta = "Homo_sapiens.GRCh37.dna.primary_assembly.fa"
	case "hg38":
		fasta = "Homo_sapiens.GRCh38.dna.primary_assembly.fa"
	default:
		return nil, fmt.Errorf("unsupported CVM reference genome")
	}
	base := path.Join("database", genome)
	keys := []string{path.Join(base, fasta), path.Join(base, fasta+".fai")}
	if requireDefaultBED {
		keys = append(keys, path.Join(base, genome+"_default.bed"))
	}
	return keys, nil
}

func validateCVMReferenceObjects(ctx context.Context, storage cvmReferenceObjectStorage, genome string, requireDefaultBED bool) error {
	keys, err := cvmReferenceObjectKeys(genome, requireDefaultBED)
	if err != nil {
		return err
	}
	for _, key := range keys {
		size, statErr := storage.stat(ctx, key)
		if statErr != nil || size <= 0 {
			// Keep provider details out of task state and client-visible diagnostics.
			return &CVMInputObjectError{
				ReasonCode: "REFERENCE_DATABASE_FAILED",
				err:        fmt.Errorf("required reference resource is missing or empty (%s)", path.Base(key)),
			}
		}
	}
	return nil
}

func (s *TaskService) preflightCVMReferences(ctx context.Context, genome, template string, inputs map[string]interface{}) error {
	config := s.cfg.Storage
	config.S3Bucket = strings.TrimSpace(config.CVMReferenceBucket)
	if config.S3Bucket == "" {
		config.S3Bucket = defaultCVMReferenceBucket
	}
	storage, err := newS3Storage(ctx, config)
	if err != nil {
		return &CVMInputObjectError{ReasonCode: "REFERENCE_DATABASE_FAILED", err: fmt.Errorf("reference object storage is unavailable")}
	}
	requireDefaultBED := false
	bed, bedErr := cvmAnalysisBEDInput(template, inputs)
	if bedErr == nil && bed == path.Join("/mnt/data/database", genome+"_default.bed") {
		requireDefaultBED = true
	}
	return validateCVMReferenceObjects(ctx, storage, genome, requireDefaultBED)
}
