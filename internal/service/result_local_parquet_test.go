package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestLocalParquetArchiveUsesDeclaredFilesAndEmptyReports(t *testing.T) {
	root := t.TempDir()
	task := &model.Task{UUID: "11111111-1111-4111-8111-111111111111"}
	dir := filepath.Join(root, task.UUID)
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"sample.snv_indel.parquet": "PAR1", "sample.roh.txt": "Chr\tStart\tEnd\n", "unlisted.str.parquet": "PAR1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{"summary": map[string]string{"snv": filepath.Join(dir, "sample.snv_indel.parquet"), "roh": filepath.Join(dir, "sample.roh.txt")}})
	manifest := filepath.Join(dir, "outputs.resolved.json")
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s := &ResultService{cfg: &config.Config{Edition: "opensource", Storage: config.StorageConfig{Provider: "local"}, Task: config.TaskConfig{ArchiveDir: root}}}
	state := s.localParquetArchiveState(task)
	if !state.Available || len(state.Tables) != 2 || state.Tables[0] != "roh" || state.Tables[1] != "snv-indel" {
		t.Fatalf("invalid local table discovery: %+v", state)
	}
	count, err := localOriginalRows(filepath.Join(dir, "sample.roh.txt"))
	if err != nil || count != 0 {
		t.Fatalf("header-only report must retain zero rows: %d %v", count, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.snv_indel.parquet")
	if err := os.WriteFile(outside, []byte("PAR1"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]string{"snv": outside})
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.localResultSources(task); err == nil {
		t.Fatal("manifest escaped the task archive")
	}
	if _, err := s.LocalBrowserFile(context.Background(), task, "snv-indel", "old-attempt", "version"); err != ErrAdjustmentConflict {
		t.Fatal("stale attempt must be rejected before dataset preparation")
	}
	s.cfg.Edition = "saas"
	if s.localBrowserStorage() {
		t.Fatal("SaaS must not use local result transport")
	}
}

func TestLocalParquetDuplicateSummaryOutputsRequireIdenticalContent(t *testing.T) {
	root := t.TempDir()
	task := &model.Task{UUID: "11111111-1111-4111-8111-111111111111"}
	dir := filepath.Join(root, task.UUID)
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(dir, "sample.snv_indel.parquet")
	right := filepath.Join(dir, "sample.snv_indel_2.parquet")
	for _, name := range []string{left, right} {
		if err := os.WriteFile(name, []byte("PAR1-identical-content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"VCF2Anno.snp_indel": left, "VCF2Anno.summary": map[string]string{"snp_indel": right},
	})
	if err := os.WriteFile(filepath.Join(dir, "outputs.resolved.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s := &ResultService{cfg: &config.Config{Edition: "opensource", Storage: config.StorageConfig{Provider: "local"}, Task: config.TaskConfig{ArchiveDir: root}}}
	for i := 0; i < 10; i++ {
		_, _, sources, err := s.localResultSources(task)
		if err != nil || sources["snv-indel"] != left {
			t.Fatalf("identical summary output should select stable source: %v %v", sources, err)
		}
	}
	if err := os.WriteFile(right, []byte("PAR1-different-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.localResultSources(task); err == nil {
		t.Fatal("conflicting results were silently deduplicated")
	}
}
