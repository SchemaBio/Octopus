package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImporterImportQCRejectsSymlinkEscape(t *testing.T) {
	archiveDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outputs.resolved.json")
	if err := os.WriteFile(outside, []byte(`{"summary":{"qc_result":{"sample_id":"secret"}}}`), 0600); err != nil {
		t.Fatalf("write outside outputs: %v", err)
	}
	link := filepath.Join(archiveDir, "outputs.resolved.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	imp := &Importer{}
	err := imp.importQC("task-1", archiveDir, &ImportResult{Counts: map[string]int{}})
	if err == nil {
		t.Fatal("expected escaped outputs.resolved.json symlink to be rejected")
	}
}

func TestImporterImportQCRejectsOversizeOutputs(t *testing.T) {
	archiveDir := t.TempDir()
	path := filepath.Join(archiveDir, "outputs.resolved.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create outputs: %v", err)
	}
	if _, err := f.Write(make([]byte, maxArchiveOutputsJSONBytes+1)); err != nil {
		f.Close()
		t.Fatalf("write outputs: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close outputs: %v", err)
	}

	imp := &Importer{}
	err = imp.importQC("task-1", archiveDir, &ImportResult{Counts: map[string]int{}})
	if err == nil {
		t.Fatal("expected oversized outputs.resolved.json to be rejected")
	}
}
