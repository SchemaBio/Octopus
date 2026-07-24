package service

import "testing"

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
