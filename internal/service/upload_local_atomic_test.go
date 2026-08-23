package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteLocalUploadFilePublishesCompleteFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "reads.fastq")
	written, err := writeLocalUploadFile(destination, strings.NewReader("ACGT"), 4, 4)
	if err != nil {
		t.Fatalf("writeLocalUploadFile: %v", err)
	}
	if written != 4 {
		t.Fatalf("written = %d, want 4", written)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "ACGT" {
		t.Fatalf("published data = %q, err = %v", data, err)
	}
}

func TestWriteLocalUploadFileRemovesTemporaryFileOnSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "reads.fastq")
	if _, err := writeLocalUploadFile(destination, strings.NewReader("ACG"), 4, 4); err == nil {
		t.Fatal("expected short upload to fail")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("partial destination exists: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files remain: entries=%v err=%v", entries, err)
	}
}

func TestWriteLocalUploadFileRejectsBytesPastDeclaredSize(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "reads.fastq")
	if _, err := writeLocalUploadFile(destination, strings.NewReader("ACGTA"), 4, 4); err == nil {
		t.Fatal("expected oversized upload to fail")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("oversized destination exists: %v", err)
	}
}
