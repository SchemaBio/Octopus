package service

import (
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestValidateUploadPolicyAcknowledgement(t *testing.T) {
	tests := []struct {
		name          string
		retentionDays int
		acknowledged  bool
		wantError     bool
	}{
		{name: "temporary storage requires acknowledgement", retentionDays: 7, wantError: true},
		{name: "temporary storage accepts acknowledgement", retentionDays: 7, acknowledged: true},
		{name: "permanent storage does not require acknowledgement"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateUploadPolicyAcknowledgement(test.retentionDays, test.acknowledged)
			if (err != nil) != test.wantError {
				t.Fatalf("validateUploadPolicyAcknowledgement() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestDataAssetExpiryKeepsBEDIndefinitely(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	if expiry := dataAssetExpiry(7, model.ReadTypeBed, now); expiry != nil {
		t.Fatalf("BED expiry = %v, want nil", expiry)
	}
	expiry := dataAssetExpiry(7, model.ReadTypeRead1, now)
	if expiry == nil || !expiry.Equal(now.AddDate(0, 0, 7)) {
		t.Fatalf("FASTQ expiry = %v, want seven days", expiry)
	}
}

func TestValidateSaaSUploadFileSize(t *testing.T) {
	tests := []struct {
		name          string
		retentionDays int
		fileSize      int64
		wantError     bool
	}{
		{name: "temporary storage accepts exactly 20 GB", retentionDays: 7, fileSize: 20 * 1024 * 1024 * 1024},
		{name: "temporary storage rejects more than 20 GB", retentionDays: 7, fileSize: 20*1024*1024*1024 + 1, wantError: true},
		{name: "permanent storage has no SaaS size limit", fileSize: 100 * 1024 * 1024 * 1024},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSaaSUploadFileSize(test.retentionDays, "reads.fastq.gz", test.fileSize)
			if (err != nil) != test.wantError {
				t.Fatalf("validateSaaSUploadFileSize() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestValidateStorageQuota(t *testing.T) {
	tests := []struct {
		name           string
		usedBytes      int64
		requestedBytes int64
		quotaBytes     int64
		wantError      bool
	}{
		{name: "unlimited accepts upload", usedBytes: 100, requestedBytes: 200},
		{name: "shared quota accepts exact remainder", usedBytes: 700, requestedBytes: 300, quotaBytes: 1000},
		{name: "shared quota rejects BED or FASTQ beyond remainder", usedBytes: 700, requestedBytes: 301, quotaBytes: 1000, wantError: true},
		{name: "already exceeded rejects upload", usedBytes: 1001, requestedBytes: 1, quotaBytes: 1000, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateStorageQuota(test.usedBytes, test.requestedBytes, test.quotaBytes)
			if (err != nil) != test.wantError {
				t.Fatalf("validateStorageQuota() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}
