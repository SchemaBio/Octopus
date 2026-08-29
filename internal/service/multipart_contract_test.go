package service

import (
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestMultipartTotalPartsAndBoundaries(t *testing.T) {
	partSize := model.MultipartPartSizeBytes
	tests := []struct {
		name     string
		fileSize int64
		want     int
	}{
		{name: "empty", fileSize: 0, want: 0},
		{name: "one part", fileSize: 1, want: 1},
		{name: "threshold", fileSize: 2 * partSize, want: 2},
		{name: "partial final", fileSize: 2*partSize + 1, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := multipartTotalParts(tt.fileSize, partSize); got != tt.want {
				t.Fatalf("multipartTotalParts(%d, %d) = %d, want %d", tt.fileSize, partSize, got, tt.want)
			}
		})
	}
}

func TestValidateMultipartPartMetadata(t *testing.T) {
	partSize := int64(32)
	fileSize := int64(65) // parts 32, 32, 1
	tests := []struct {
		name      string
		part      int
		etag      string
		size      int64
		wantError string
	}{
		{name: "first full part", part: 1, etag: "etag-1", size: 32},
		{name: "final short part", part: 3, etag: "etag-3", size: 1},
		{name: "blank etag", part: 1, etag: " ", size: 32, wantError: "invalid multipart part"},
		{name: "part zero", part: 0, etag: "etag", size: 32, wantError: "invalid multipart part"},
		{name: "non-final short part", part: 1, etag: "etag", size: 31, wantError: "multipart part size mismatch"},
		{name: "final wrong size", part: 3, etag: "etag", size: 2, wantError: "last multipart part size mismatch"},
		{name: "out of range", part: 4, etag: "etag", size: 1, wantError: "invalid multipart part"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMultipartPartMetadata(tt.part, tt.etag, tt.size, fileSize, partSize)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestValidateRecordedMultipartPartIsIdempotentOnlyForSameETag(t *testing.T) {
	existing := &model.UploadMultipartPart{PartNumber: 2, ETag: "etag-2", Size: 32}
	if err := validateRecordedMultipartPart(existing, "  etag-2 ", 32, 2); err != nil {
		t.Fatalf("same ETag/size should be idempotent: %v", err)
	}
	for _, test := range []struct {
		name string
		etag string
		size int64
	}{
		{name: "different etag", etag: "etag-other", size: 32},
		{name: "different size", etag: "etag-2", size: 31},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateRecordedMultipartPart(existing, test.etag, test.size, 2); err == nil {
				t.Fatal("different ETag or size must be rejected")
			}
		})
	}
}

func TestCollectMultipartPartsRejectsMissingOrMalformedParts(t *testing.T) {
	partSize := int64(32)
	fileSize := int64(65)
	valid := []model.UploadMultipartPart{
		{PartNumber: 1, ETag: "etag-1", Size: 32},
		{PartNumber: 2, ETag: "etag-2", Size: 32},
		{PartNumber: 3, ETag: "etag-3", Size: 1},
	}
	parts, err := collectMultipartParts(valid, 3, fileSize, partSize)
	if err != nil || len(parts) != 3 {
		t.Fatalf("valid parts = %#v, err=%v", parts, err)
	}
	if _, err := collectMultipartParts(valid[:2], 3, fileSize, partSize); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("missing part should be rejected, got %v", err)
	}
	missing := append([]model.UploadMultipartPart(nil), valid...)
	missing[1].PartNumber = 3
	if _, err := collectMultipartParts(missing, 3, fileSize, partSize); err == nil || !strings.Contains(err.Error(), "part 2 is missing") {
		t.Fatalf("out-of-order part should be rejected, got %v", err)
	}
	malformed := append([]model.UploadMultipartPart(nil), valid...)
	malformed[2].ETag = ""
	if _, err := collectMultipartParts(malformed, 3, fileSize, partSize); err == nil || !strings.Contains(err.Error(), "invalid multipart part") {
		t.Fatalf("empty ETag should be rejected, got %v", err)
	}
}

func TestValidateMultipartStorageKeyScopesTenantAndSelfDeployedKeys(t *testing.T) {
	orgID := "f83165ee-e23c-4bf1-a42e-78ac39c6f1ba"
	job := &model.UploadJob{ExternalOrgID: orgID}
	tests := []struct {
		name string
		key  string
		good bool
	}{
		{name: "tenant key", key: "organizations/" + orgID + "/uploads/fastq/file.fastq.gz", good: true},
		{name: "other tenant", key: "organizations/11111111-1111-1111-1111-111111111111/uploads/file", good: false},
		{name: "traversal", key: "organizations/" + orgID + "/uploads/../other/file", good: false},
		{name: "absolute", key: "/organizations/" + orgID + "/uploads/file", good: false},
		{name: "backslash", key: "organizations\\" + orgID + "\\uploads\\file", good: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := &model.UploadFile{StorageKey: tt.key}
			err := validateMultipartStorageKey(file, job)
			if (err == nil) != tt.good {
				t.Fatalf("validateMultipartStorageKey(%q) error=%v, good=%v", tt.key, err, tt.good)
			}
		})
	}
	if err := validateMultipartStorageKey(&model.UploadFile{StorageKey: "uploads/file.fastq.gz"}, &model.UploadJob{}); err != nil {
		t.Fatalf("self-deployed upload key should be accepted: %v", err)
	}
}

func TestValidateMultipartWritableRejectsTerminalRecords(t *testing.T) {
	completed := &model.UploadFile{Status: model.FileStatusCompleted}
	if err := validateMultipartWritable(completed, &model.UploadJob{}); err == nil || !strings.Contains(err.Error(), "already completed") {
		t.Fatalf("completed file should not be writable, got %v", err)
	}
	for _, status := range []model.FileStatus{model.FileStatusDeleting, model.FileStatusDeleted} {
		if err := validateMultipartWritable(&model.UploadFile{Status: status}, &model.UploadJob{}); err == nil {
			t.Fatalf("file status %q should not be writable", status)
		}
	}
	for _, status := range []model.UploadJobStatus{model.UploadJobStatusDeleting, model.UploadJobStatusDeleted} {
		if err := validateMultipartWritable(&model.UploadFile{Status: model.FileStatusUploading}, &model.UploadJob{Status: status}); err == nil {
			t.Fatalf("job status %q should not permit multipart writes", status)
		}
	}
}
