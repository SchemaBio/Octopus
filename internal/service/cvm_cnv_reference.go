package service

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
)

// Custom CNV baselines are stored as unsigned COS object URLs. MiniWDL would
// otherwise download that URL itself, and a private bucket rejects the request.
// Stage the object with the same presigned download plan as FASTQ and BED, then
// hand MiniWDL a local path.
func stageCustomCNVReferences(ctx context.Context, storage cvmInputObjectStorage, orgID, bucketHost string, inputs map[string]interface{}, downloads []model.CVMInputDownload, expiry time.Duration) ([]model.CVMInputDownload, error) {
	targets := make(map[string]struct{}, len(downloads))
	byKey := make(map[string]string, len(downloads))
	for _, item := range downloads {
		targets[item.Target] = struct{}{}
		byKey[item.ObjectKey] = item.Target
	}
	for _, inputKey := range []string{"SingleWES.cnvkit_reference", "TrioWES.cnvkit_reference"} {
		value, _ := inputs[inputKey].(string)
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(value, "/mnt/data/database/") {
			continue
		}
		if strings.HasPrefix(value, "/mnt/data/inputs/") {
			if _, ok := targets[path.Clean(value)]; !ok {
				return nil, fmt.Errorf("CNV baseline reference is not backed by a staged download")
			}
			continue
		}
		objectKey, err := tenantBaselineReferenceKey(orgID, bucketHost, value)
		if err != nil {
			return nil, err
		}
		target, ok := byKey[objectKey]
		if !ok {
			size, statErr := storage.stat(ctx, objectKey)
			if statErr != nil || size <= 0 {
				return nil, &CVMInputObjectError{ReasonCode: cvmReasonInputObjectUnavailable,
					err: fmt.Errorf("CNV baseline reference is unavailable")}
			}
			signed, signErr := storage.presignDownloadWithExpiry(ctx, objectKey, "reference.cnn", expiry)
			if signErr != nil {
				return nil, &CVMInputObjectError{ReasonCode: cvmReasonInputObjectUnavailable,
					err: fmt.Errorf("CNV baseline reference could not be prepared")}
			}
			target = path.Join("/mnt/data/inputs", path.Base(path.Dir(objectKey))+"-reference.cnn")
			if _, exists := targets[target]; exists {
				return nil, fmt.Errorf("CNV baseline reference target conflicts with another input")
			}
			downloads = append(downloads, model.CVMInputDownload{
				ObjectKey: objectKey, URL: signed, Target: target,
				ExpectedSizeBytes: size, ValidationRole: "cnv_reference",
			})
			targets[target] = struct{}{}
			byKey[objectKey] = target
		}
		inputs[inputKey] = target
	}
	return downloads, nil
}

func tenantBaselineReferenceKey(orgID, bucketHost, value string) (string, error) {
	key := strings.TrimSpace(value)
	if strings.Contains(key, "://") {
		parsed, err := url.Parse(key)
		if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
			return "", fmt.Errorf("CNV baseline reference URL is invalid")
		}
		if !strings.EqualFold(parsed.Host, strings.TrimSpace(bucketHost)) {
			return "", fmt.Errorf("CNV baseline reference is not stored in the tenant object bucket")
		}
		key = strings.TrimPrefix(parsed.EscapedPath(), "/")
		if unescaped, err := url.PathUnescape(key); err == nil {
			key = unescaped
		}
	}
	if strings.Contains(key, "\\") || strings.Contains(key, "..") || path.Clean(key) != key {
		return "", fmt.Errorf("CNV baseline reference object key is invalid")
	}
	prefix := path.Join("organizations", strings.TrimSpace(orgID), "baselines") + "/"
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, "/reference.cnn") {
		return "", fmt.Errorf("CNV baseline reference is outside the tenant baseline prefix")
	}
	baselineID := path.Base(path.Dir(key))
	if _, err := uuid.Parse(baselineID); err != nil || path.Join(prefix, baselineID, "reference.cnn") != key {
		return "", fmt.Errorf("CNV baseline reference object key is invalid")
	}
	return key, nil
}
