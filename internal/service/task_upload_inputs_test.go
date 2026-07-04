package service

import (
	"testing"

	"github.com/bioinfo/schema-platform/internal/model"
)

func TestApplyUploadFilesToInputsReplacesPendingStorageKeys(t *testing.T) {
	inputs := map[string]interface{}{
		"fastq_r1": "user-folder/job-1/R1.fastq.gz",
		"fastq_r2": "user-folder/job-1/R2.fastq.gz",
	}
	files := []model.UploadFile{
		{ReadType: model.ReadTypeRead1, StorageKey: "/data/uploads/user-folder/job-1/R1.fastq.gz"},
		{ReadType: model.ReadTypeRead2, StorageKey: "/data/uploads/user-folder/job-1/R2.fastq.gz"},
	}

	if !applyUploadFilesToInputs(inputs, files) {
		t.Fatal("expected upload file paths to update task inputs")
	}
	if inputs["fastq_r1"] != "/data/uploads/user-folder/job-1/R1.fastq.gz" {
		t.Fatalf("unexpected fastq_r1: %v", inputs["fastq_r1"])
	}
	if inputs["fastq_r2"] != "/data/uploads/user-folder/job-1/R2.fastq.gz" {
		t.Fatalf("unexpected fastq_r2: %v", inputs["fastq_r2"])
	}
}

func TestApplyUploadFilesToInputsIsStableWhenAlreadyCurrent(t *testing.T) {
	inputs := map[string]interface{}{
		"bed_file": "/data/uploads/user-folder/job-1/panel.bed",
	}
	files := []model.UploadFile{
		{ReadType: model.ReadTypeBed, StorageKey: "/data/uploads/user-folder/job-1/panel.bed"},
	}

	if applyUploadFilesToInputs(inputs, files) {
		t.Fatal("expected no change when task input already points to current upload file")
	}
}
