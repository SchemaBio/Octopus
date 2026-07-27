package service

import (
	"fmt"
	"path"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
)

const tenantStorageLayoutVersion = 1

func normalizeTenantID(value string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("org_id must be a valid UUID")
	}
	return parsed.String(), nil
}

func tenantStoragePrefix(orgID string) (string, error) {
	orgID, err := normalizeTenantID(orgID)
	if err != nil {
		return "", err
	}
	return path.Join("organizations", orgID), nil
}

func tenantUploadCategory(fileType model.UploadFileType, referenceGenome string) (string, error) {
	switch fileType {
	case model.UploadFileTypeFastqPaired:
		return "fastq/paired", nil
	case model.UploadFileTypeFastqSingle:
		return "fastq/single", nil
	case model.UploadFileTypeBed:
		genome, err := normalizeReferenceGenome(referenceGenome)
		if err != nil {
			return "", err
		}
		return path.Join("bed", genome), nil
	case model.UploadFileTypeOther:
		return "other", nil
	default:
		return "", fmt.Errorf("unsupported file_type %q", fileType)
	}
}

func tenantUploadObjectKey(orgID, jobUUID string, fileType model.UploadFileType, referenceGenome, fileName string) (string, error) {
	prefix, err := tenantStoragePrefix(orgID)
	if err != nil {
		return "", err
	}
	category, err := tenantUploadCategory(fileType, referenceGenome)
	if err != nil {
		return "", err
	}
	if _, err := uuid.Parse(jobUUID); err != nil {
		return "", fmt.Errorf("job UUID is invalid")
	}
	if err := validateUploadFilename(fileName); err != nil {
		return "", err
	}
	return path.Join(prefix, "uploads", category, jobUUID, fileName), nil
}

func tenantDirectoryMarkers(orgID string) ([]string, error) {
	prefix, err := tenantStoragePrefix(orgID)
	if err != nil {
		return nil, err
	}
	directories := []string{
		"uploads/fastq/paired",
		"uploads/fastq/single",
		"uploads/bed/GRCh37",
		"uploads/bed/GRCh38",
		"uploads/other",
		"workflows",
		"reports",
	}
	markers := make([]string, 0, len(directories))
	for _, directory := range directories {
		markers = append(markers, path.Join(prefix, directory, ".keep"))
	}
	return markers, nil
}
