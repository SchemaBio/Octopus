package service

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

const (
	cvmReasonInputObjectUnavailable  = "INPUT_OBJECT_UNAVAILABLE"
	cvmReasonInputObjectSizeMismatch = "INPUT_OBJECT_SIZE_MISMATCH"
	cvmReasonInputDownloadFailed     = "INPUT_DOWNLOAD_FAILED"
	cvmReasonInputValidationInfra    = "INPUT_VALIDATION_INFRA_FAILED"
	cvmReasonFastqInvalid            = "USER_INPUT_FASTQ_INVALID"
	cvmReasonFastqPairMismatch       = "USER_INPUT_FASTQ_PAIR_MISMATCH"
	cvmReasonBEDInvalid              = "USER_INPUT_BED_INVALID"
)

// CVMInputObjectError is a platform-owned failure discovered before a spot
// request is sent. It intentionally exposes no signed URL or object contents.
type CVMInputObjectError struct {
	ReasonCode string
	err        error
}

func (e *CVMInputObjectError) Error() string {
	if e == nil || e.err == nil {
		return "CVM input object preflight failed"
	}
	return e.err.Error()
}

func (e *CVMInputObjectError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *CVMInputObjectError) CVMReasonCode() string {
	if e == nil {
		return ""
	}
	return e.ReasonCode
}

type cvmInputObjectStorage interface {
	stat(context.Context, string) (int64, error)
	presignDownloadWithExpiry(context.Context, string, string, time.Duration) (string, error)
}

type cvmPreparedDownload struct {
	asset        *model.DataAsset
	target       string
	role         string
	pairKey      string
	expectedSize int64
}

func prepareCVMInputDownloads(ctx context.Context, storage cvmInputObjectStorage, assets []*model.DataAsset, directLinks map[uint][]model.TaskDataAsset, expiry time.Duration) ([]model.CVMInputDownload, error) {
	return prepareCVMInputDownloadsForContract(ctx, storage, assets, directLinks, expiry, "germline-v2")
}

func prepareCVMInputDownloadsForContract(ctx context.Context, storage cvmInputObjectStorage, assets []*model.DataAsset, directLinks map[uint][]model.TaskDataAsset, expiry time.Duration, contract string) ([]model.CVMInputDownload, error) {
	strictV2 := contract == "germline-v2"
	prepared := make([]cvmPreparedDownload, 0, len(assets))
	for _, asset := range assets {
		role, pairKey := "", ""
		if strictV2 {
			var err error
			role, pairKey, err = cvmValidationMetadata(asset, directLinks[asset.ID])
			if err != nil {
				return nil, err
			}
		}
		if asset.FileSize <= 0 {
			return nil, &CVMInputObjectError{ReasonCode: cvmReasonInputObjectSizeMismatch,
				err: fmt.Errorf("CVM input object %s has no confirmed positive size", asset.UUID)}
		}
		prepared = append(prepared, cvmPreparedDownload{
			asset: asset, target: path.Join("/mnt/data/inputs", asset.UUID+"-"+safeCVMInputName(asset.FileName)),
			role: role, pairKey: pairKey, expectedSize: asset.FileSize,
		})
	}
	downloads := make([]model.CVMInputDownload, 0, len(prepared))
	for _, item := range prepared {
		download := model.CVMInputDownload{ObjectKey: item.asset.StorageKey, Target: item.target}
		if strictV2 {
			download.ExpectedSizeBytes = item.expectedSize
			download.ValidationRole = item.role
			download.PairKey = item.pairKey
		}
		downloads = append(downloads, download)
	}
	if strictV2 {
		if err := validateCVMDownloadPairs(downloads); err != nil {
			return nil, err
		}
	}
	// Complete every HEAD check before minting any URL. A failed preflight must
	// never leave a partially refreshed request that can reach provisioning.
	for _, item := range prepared {
		actual, err := storage.stat(ctx, item.asset.StorageKey)
		if err != nil {
			return nil, &CVMInputObjectError{ReasonCode: cvmReasonInputObjectUnavailable,
				err: fmt.Errorf("CVM input object %s is unavailable", item.asset.UUID)}
		}
		if actual <= 0 || actual != item.expectedSize {
			return nil, &CVMInputObjectError{ReasonCode: cvmReasonInputObjectSizeMismatch,
				err: fmt.Errorf("CVM input object %s size does not match the completed upload", item.asset.UUID)}
		}
	}

	for i, item := range prepared {
		name := safeCVMInputName(item.asset.FileName)
		url, err := storage.presignDownloadWithExpiry(ctx, item.asset.StorageKey, name, expiry)
		if err != nil {
			return nil, &CVMInputObjectError{ReasonCode: cvmReasonInputObjectUnavailable,
				err: fmt.Errorf("CVM input object %s could not be prepared", item.asset.UUID)}
		}
		downloads[i].URL = url
	}
	return downloads, nil
}

func cvmValidationMetadata(asset *model.DataAsset, links []model.TaskDataAsset) (string, string, error) {
	if asset == nil {
		return "", "", fmt.Errorf("CVM input asset is required")
	}
	if asset.ReadType == model.ReadTypeBed {
		return "bed", "", nil
	}
	for _, link := range links {
		switch link.InputRole {
		case model.TaskAssetRoleTrioRead1:
			return "fastq_r1", "trio:" + strconv.Itoa(link.InputIndex), nil
		case model.TaskAssetRoleTrioRead2:
			return "fastq_r2", "trio:" + strconv.Itoa(link.InputIndex), nil
		case model.TaskAssetRoleCNVRead1:
			return "fastq_r1", "cnv_baseline:" + strconv.Itoa(link.InputIndex), nil
		case model.TaskAssetRoleCNVRead2:
			return "fastq_r2", "cnv_baseline:" + strconv.Itoa(link.InputIndex), nil
		case model.TaskAssetRoleCNVBED, model.TaskAssetRoleAnalysisBED:
			return "bed", "", nil
		}
	}
	switch asset.ReadType {
	case model.ReadTypeRead1, model.ReadTypeSingle:
		return "fastq_r1", "single:0", nil
	case model.ReadTypeRead2:
		return "fastq_r2", "single:0", nil
	default:
		return "", "", fmt.Errorf("CVM input asset %s has an unsupported validation role", asset.UUID)
	}
}

func validateCVMDownloadPairs(downloads []model.CVMInputDownload) error {
	type pair struct{ r1, r2 int }
	pairs := make(map[string]pair)
	for _, item := range downloads {
		switch item.ValidationRole {
		case "bed":
			if item.PairKey != "" {
				return fmt.Errorf("BED validation role must not have a pair key")
			}
		case "fastq_r1", "fastq_r2":
			if strings.TrimSpace(item.PairKey) == "" {
				return fmt.Errorf("FASTQ validation role requires a pair key")
			}
			value := pairs[item.PairKey]
			if item.ValidationRole == "fastq_r1" {
				value.r1++
			} else {
				value.r2++
			}
			pairs[item.PairKey] = value
		default:
			return fmt.Errorf("CVM input has an unsupported validation role")
		}
	}
	for _, pair := range pairs {
		if pair.r1 != 1 || pair.r2 != 1 {
			return fmt.Errorf("CVM FASTQ pair is incomplete or duplicated")
		}
	}
	if len(pairs) == 0 {
		return fmt.Errorf("CVM execution requires at least one complete FASTQ pair")
	}
	return nil
}
