package model

import "testing"

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
}
