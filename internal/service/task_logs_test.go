package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTaskLogFileRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.log")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatalf("write outside log: %v", err)
	}
	link := filepath.Join(base, "octopus.log")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	if _, err := readTaskLogFile(base, link); err == nil {
		t.Fatal("expected symlink escaping task output to be rejected")
	}
}

func TestReadTaskLogFileRejectsOversizeLog(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "octopus.log")
	if err := os.WriteFile(path, make([]byte, maxTaskLogBytes+1), 0600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	if _, err := readTaskLogFile(base, path); err == nil {
		t.Fatal("expected oversized task log to be rejected")
	}
}

func TestReadTaskLogFileAcceptsRegularLog(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "octopus.log")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	got, err := readTaskLogFile(base, path)
	if err != nil {
		t.Fatalf("expected regular log: %v", err)
	}
	if got != "hello" {
		t.Fatalf("unexpected log content: %q", got)
	}
}
