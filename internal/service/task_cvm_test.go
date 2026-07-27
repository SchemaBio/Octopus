package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestReplaceInputStringRecurses(t *testing.T) {
	inputs := map[string]interface{}{
		"direct": "objects/r1.fastq.gz",
		"nested": []interface{}{
			map[string]interface{}{"file": "objects/r1.fastq.gz"},
			"unchanged",
		},
	}
	replaced := replaceInputString(inputs, "objects/r1.fastq.gz", "/data/inputs/r1.fastq.gz").(map[string]interface{})
	if replaced["direct"] != "/data/inputs/r1.fastq.gz" {
		t.Fatalf("direct input was not replaced: %#v", replaced)
	}
	nested := replaced["nested"].([]interface{})
	if nested[0].(map[string]interface{})["file"] != "/data/inputs/r1.fastq.gz" || nested[1] != "unchanged" {
		t.Fatalf("nested input replacement is wrong: %#v", nested)
	}
}

func TestSafeCVMInputName(t *testing.T) {
	if got := safeCVMInputName(`../sample R1;rm.fastq.gz`); got != "sample_R1_rm.fastq.gz" {
		t.Fatalf("unexpected safe input name: %q", got)
	}
}

func TestStructuredResultFileSelection(t *testing.T) {
	for _, name := range []string{"sample.snv_indel.tsv", "sample.region.cnvanno.txt", "sample.str.txt", "sample.roh.tsv"} {
		if !isStructuredResultFile(name) {
			t.Fatalf("expected structured result file: %s", name)
		}
	}
	for _, name := range []string{"sample.bam", "sample.vcf.gz", "sample.snv_indel.parquet", "workflow.log"} {
		if isStructuredResultFile(name) {
			t.Fatalf("unexpected structured result file: %s", name)
		}
	}
}

func TestSepiidaWorkflowUUIDUsesCurrentCVMExecutionAttempt(t *testing.T) {
	task := &model.Task{UUID: "9e906aba-75f0-4b68-a355-5138b0f07c42", Executor: model.ExecutorCVM, ExecutionAttemptID: "2897aa33-054d-4a00-88c6-40701d0cf491"}
	if got := sepiidaWorkflowUUID(task); got != task.ExecutionAttemptID {
		t.Fatalf("expected current execution attempt, got %q", got)
	}
	task.Executor = model.ExecutorLocal
	if got := sepiidaWorkflowUUID(task); got != task.UUID {
		t.Fatalf("community executor must keep task UUID, got %q", got)
	}
}

func TestCVMAttemptTerminalStates(t *testing.T) {
	for _, state := range []string{"TERMINATED", "reclaimed", "launch_failed"} {
		if !cvmAttemptStateTerminal(state) {
			t.Fatalf("expected terminal state %q", state)
		}
	}
	if cvmAttemptStateTerminal("RUNNING") {
		t.Fatal("RUNNING must not be terminal")
	}
}

func TestCVMStateEventOnlyMatchesCurrentAttempt(t *testing.T) {
	task := &model.Task{ExecutionAttemptID: "2897aa33-054d-4a00-88c6-40701d0cf491"}
	current := model.CVMStateEvent{AttemptID: task.ExecutionAttemptID}
	stale := model.CVMStateEvent{AttemptID: "8db870b7-a65f-48ea-9d9c-8e5f799ae213"}
	if !cvmStateEventMatchesCurrentAttempt(task, current) {
		t.Fatal("current execution attempt should match")
	}
	if cvmStateEventMatchesCurrentAttempt(task, stale) {
		t.Fatal("stale execution attempt must be ignored")
	}
}

func TestCVMTaskNeedsCancelWithoutKnownInstance(t *testing.T) {
	task := &model.Task{Executor: model.ExecutorCVM, ExecutionAttemptID: "2897aa33-054d-4a00-88c6-40701d0cf491"}
	if !cvmTaskNeedsCancel(task) {
		t.Fatal("a dispatching attempt must be cancellable before its instance ID is known")
	}
	task.Executor = model.ExecutorLocal
	if cvmTaskNeedsCancel(task) {
		t.Fatal("community execution must not call the Squid CVM cancel endpoint")
	}
}

func TestCVMFinalManifestKeyUsesStableTaskRoot(t *testing.T) {
	task := &model.Task{
		UUID:               "9e906aba-75f0-4b68-a355-5138b0f07c42",
		ExternalOrgID:      "f83165ee-e23c-4bf1-a42e-78ac39c6f1ba",
		ExecutionAttemptID: "2897aa33-054d-4a00-88c6-40701d0cf491",
	}
	got, err := cvmFinalManifestKey(task)
	if err != nil {
		t.Fatal(err)
	}
	want := "organizations/f83165ee-e23c-4bf1-a42e-78ac39c6f1ba/workflows/9e906aba-75f0-4b68-a355-5138b0f07c42/final.json"
	if got != want {
		t.Fatalf("unexpected final manifest key: %q", got)
	}
}

func TestTaskArchiveDirSeparatesCVMExecutionAttempts(t *testing.T) {
	base := filepath.Join("var", "archive")
	task := &model.Task{
		UUID:               "9e906aba-75f0-4b68-a355-5138b0f07c42",
		Executor:           model.ExecutorCVM,
		ExecutionAttemptID: "2897aa33-054d-4a00-88c6-40701d0cf491",
	}
	got, err := taskArchiveDir(base, task)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, task.UUID, "attempts", task.ExecutionAttemptID)
	if got != want {
		t.Fatalf("unexpected CVM archive directory: %q", got)
	}
	task.Executor = model.ExecutorLocal
	got, err = taskArchiveDir(base, task)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, task.UUID); got != want {
		t.Fatalf("community archive directory changed: %q", got)
	}
}

func TestValidatedCOSArchivePrefixRejectsStaleAttempt(t *testing.T) {
	task := &model.Task{
		UUID:               "9e906aba-75f0-4b68-a355-5138b0f07c42",
		ExternalOrgID:      "f83165ee-e23c-4bf1-a42e-78ac39c6f1ba",
		Executor:           model.ExecutorCVM,
		ExecutionAttemptID: "2897aa33-054d-4a00-88c6-40701d0cf491",
	}
	storageCfg := config.StorageConfig{S3Bucket: "bucket-1250000000", S3Region: "ap-guangzhou"}
	metadata := model.SepiidaArchiveMetadata{
		ArchiveBase:        "https://bucket-1250000000.cos.ap-guangzhou.myqcloud.com/organizations/" + task.ExternalOrgID + "/workflows/" + task.UUID + "/attempts",
		ArchivePrefix:      task.ExecutionAttemptID,
		OutputsResolvedKey: task.ExecutionAttemptID + "/outputs.resolved.json",
	}
	got, err := validatedCOSArchivePrefix(task, metadata, storageCfg)
	if err != nil {
		t.Fatal(err)
	}
	want := "organizations/" + task.ExternalOrgID + "/workflows/" + task.UUID + "/attempts/" + task.ExecutionAttemptID
	if got != want {
		t.Fatalf("unexpected archive prefix: %q", got)
	}
	metadata.ArchivePrefix = "8db870b7-a65f-48ea-9d9c-8e5f799ae213"
	if _, err := validatedCOSArchivePrefix(task, metadata, storageCfg); err == nil || !strings.Contains(err.Error(), "current execution attempt") {
		t.Fatalf("expected stale attempt to be rejected, got %v", err)
	}
}
