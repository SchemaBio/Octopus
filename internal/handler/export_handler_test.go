package handler

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRegularFileInsideBaseRejectsOutsideFile(t *testing.T) {
	base := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "result.vcf")
	if err := os.WriteFile(outsideFile, []byte("vcf"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if _, err := resolveRegularFileInsideBase(base, outsideFile); err == nil {
		t.Fatal("expected outside file to be rejected")
	}
}

func TestResolveRegularFileInsideBaseAcceptsRegularFile(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "result.vcf")
	if err := os.WriteFile(filePath, []byte("vcf"), 0600); err != nil {
		t.Fatalf("write inside file: %v", err)
	}

	got, err := resolveRegularFileInsideBase(base, filePath)
	if err != nil {
		t.Fatalf("expected inside regular file to be accepted: %v", err)
	}
	if got == "" {
		t.Fatal("expected resolved path")
	}
}
