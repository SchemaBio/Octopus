package service

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
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

func TestMatchedPairCompleteRequiresBothReads(t *testing.T) {
	if matchedPairComplete(nil) {
		t.Fatal("nil pair must not be ready")
	}
	if matchedPairComplete(&model.MatchedPair{R1Path: "R1.fastq.gz"}) {
		t.Fatal("partial pair must not be ready")
	}
	if !matchedPairComplete(&model.MatchedPair{R1Path: "R1.fastq.gz", R2Path: "R2.fastq.gz"}) {
		t.Fatal("complete pair should be ready")
	}
}

func TestApplySampleMatchedPairToInputsReplacesPreviousMatch(t *testing.T) {
	inputs := map[string]interface{}{
		"fastq_r1": "auto_R1.fastq.gz",
		"fastq_r2": "auto_R2.fastq.gz",
	}
	pair := &model.MatchedPair{R1Path: "manual_R1.fastq.gz", R2Path: "manual_R2.fastq.gz"}
	if !applySampleMatchedPairToInputs(inputs, pair) {
		t.Fatal("expected effective sample pair to update task inputs")
	}
	if inputs["fastq_r1"] != pair.R1Path || inputs["fastq_r2"] != pair.R2Path {
		t.Fatalf("unexpected sample inputs: %#v", inputs)
	}
}
