package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
)

func TestResolveArchiveRegularFileRejectsOutsidePath(t *testing.T) {
	archiveDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if _, err := resolveArchiveRegularFile(archiveDir, outsideFile); err == nil {
		t.Fatal("expected outside archive path to be rejected")
	}
}

func TestQueryOutputByKeyOnlyMarksRegularArchiveFileExisting(t *testing.T) {
	root := t.TempDir()
	taskID := "task-1"
	archiveDir := filepath.Join(root, taskID)
	if err := os.MkdirAll(archiveDir, 0700); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "outputs.resolved.json"), []byte(`{"outputs":{"vcf":"/work/result.vcf"}}`), 0600); err != nil {
		t.Fatalf("write outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "result.vcf"), []byte("vcf"), 0600); err != nil {
		t.Fatalf("write result: %v", err)
	}

	archiver := NewArchiver(&config.Config{Task: config.TaskConfig{ArchiveDir: root}})
	result, err := archiver.QueryOutputByKey(taskID, "outputs.vcf")
	if err != nil {
		t.Fatalf("QueryOutputByKey returned error: %v", err)
	}
	if !result.Exists || result.ArchivePath == "" {
		t.Fatalf("expected regular archive file to be marked existing: %#v", result)
	}
	if !strings.HasPrefix(result.ArchivePath, archiveDir) {
		t.Fatalf("archive path escaped task archive dir: %q", result.ArchivePath)
	}
}

func TestListArchivedFilesSkipsDirectories(t *testing.T) {
	root := t.TempDir()
	taskID := "task-1"
	archiveDir := filepath.Join(root, taskID)
	if err := os.MkdirAll(filepath.Join(archiveDir, "nested"), 0700); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "result.vcf"), []byte("vcf"), 0600); err != nil {
		t.Fatalf("write result: %v", err)
	}

	archiver := NewArchiver(&config.Config{Task: config.TaskConfig{ArchiveDir: root}})
	files, err := archiver.ListArchivedFiles(taskID)
	if err != nil {
		t.Fatalf("ListArchivedFiles returned error: %v", err)
	}
	if len(files) != 1 || files[0] != "result.vcf" {
		t.Fatalf("unexpected archived files: %#v", files)
	}
}

func TestReadOutputsFallsBackToOutputsJSON(t *testing.T) {
	root := t.TempDir()
	taskID := "task-1"
	archiveDir := filepath.Join(root, taskID)
	if err := os.MkdirAll(archiveDir, 0700); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "outputs.json"), []byte(`{"task":{"txt":"result.txt"}}`), 0600); err != nil {
		t.Fatalf("write outputs: %v", err)
	}

	archiver := NewArchiver(&config.Config{Task: config.TaskConfig{ArchiveDir: root}})
	outputs, err := archiver.ReadOutputs(taskID)
	if err != nil {
		t.Fatalf("ReadOutputs returned error: %v", err)
	}
	if outputs["task"] == nil {
		t.Fatalf("expected fallback outputs.json to be parsed: %#v", outputs)
	}
}
