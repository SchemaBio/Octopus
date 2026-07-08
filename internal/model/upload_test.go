package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUploadFileToResponseDoesNotExposeStorageKey(t *testing.T) {
	resp := UploadFileToResponse(&UploadFile{
		UUID:       "file-1",
		JobUUID:    "job-1",
		FileName:   "reads.fastq",
		StorageKey: "/srv/private/uploads/user/job/reads.fastq",
		Status:     FileStatusCompleted,
	})
	if resp.StorageKey != "" {
		t.Fatalf("expected storage key to be omitted from public response, got %q", resp.StorageKey)
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(encoded), "storage_key") || strings.Contains(string(encoded), "/srv/private") {
		t.Fatalf("serialized response exposed storage key: %s", encoded)
	}
}
