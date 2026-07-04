package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeLocalUploadPathRejectsEscape(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.fastq")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if _, err := safeLocalUploadPath(base, outside); err == nil {
		t.Fatal("expected path outside upload storage to be rejected")
	}
}

func TestSafeLocalUploadPathRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.fastq")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(base, "link.fastq")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	if _, err := safeLocalUploadPath(base, link); err == nil {
		t.Fatal("expected symlink escaping upload storage to be rejected")
	}
}

func TestSafeLocalUploadPathAcceptsRegularFile(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "reads.fastq")
	if err := os.WriteFile(path, []byte("@r\nA\n+\n!\n"), 0600); err != nil {
		t.Fatalf("write upload file: %v", err)
	}
	got, err := safeLocalUploadPath(base, path)
	if err != nil {
		t.Fatalf("expected regular file to be accepted: %v", err)
	}
	if got == "" {
		t.Fatal("expected resolved path")
	}
}
