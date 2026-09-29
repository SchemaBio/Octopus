package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestQCMemberRolesKeepsTrioOrderAndAvailability(t *testing.T) {
	roles := resultMemberRoles([]interface{}{"proband", "father", "mother"})
	for index, want := range []string{"proband", "father", "mother"} {
		if got := memberRoleAt(roles, index, true); got != want {
			t.Fatalf("memberRoleAt(%d) = %q, want %q", index, got, want)
		}
	}
	if got := memberRoleAt(nil, 1, true); got != "father" {
		t.Fatalf("trio fallback role = %q, want father", got)
	}

	availability := qcMetricAvailability(map[string]interface{}{
		"fastp": map[string]interface{}{
			"after_filtering": map[string]interface{}{"total_reads": float64(0)},
		},
	})
	var parsed map[string]bool
	if err := json.Unmarshal([]byte(availability), &parsed); err != nil {
		t.Fatalf("decode availability: %v", err)
	}
	if !parsed["totalReads"] {
		t.Fatal("present zero-valued total_reads was not marked available")
	}
	if parsed["averageDepth"] {
		t.Fatal("missing average_depth was marked available")
	}
}

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

func TestMarshalImportAuditJSONKeepsNilCollectionsValid(t *testing.T) {
	if got := marshalImportAuditJSON([]string(nil)); got != "[]" {
		t.Fatalf("nil source-file list JSON = %q, want []", got)
	}
	if got := marshalImportAuditJSON(map[string]int(nil)); got != "{}" {
		t.Fatalf("nil counts JSON = %q, want {}", got)
	}
	if got := marshalImportAuditJSON(nil); got != "null" {
		t.Fatalf("nil value JSON = %q, want null", got)
	}
}
