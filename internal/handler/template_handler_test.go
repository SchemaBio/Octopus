package handler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bioinfo/schema-platform/internal/model"
)

func TestSafePublicTemplateRemovesServerPath(t *testing.T) {
	got := safePublicTemplate(model.Template{Name: "wes", Path: "/srv/templates/wes.wdl"})
	if got.Path != "" {
		t.Fatalf("expected public template path to be removed, got %q", got.Path)
	}
}

func TestReadTemplateFileRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	templateDir = dir
	outside := filepath.Join(t.TempDir(), "secret.wdl")
	if err := os.WriteFile(outside, []byte("workflow leak {}"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(dir, "escape.wdl")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	if _, err := readTemplateFile(link); err == nil {
		t.Fatal("expected symlink escaping template directory to be rejected")
	}
}

func TestReadTemplateFileAcceptsRegularTemplate(t *testing.T) {
	dir := t.TempDir()
	templateDir = dir
	path := filepath.Join(dir, "ok.wdl")
	if err := os.WriteFile(path, []byte("workflow ok { input { String sample } }"), 0600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	content, err := readTemplateFile(path)
	if err != nil {
		t.Fatalf("expected regular template to be read: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected template content")
	}
}

func TestReadTemplateFileRejectsOversizeTemplate(t *testing.T) {
	dir := t.TempDir()
	templateDir = dir
	path := filepath.Join(dir, "huge.wdl")
	if err := os.WriteFile(path, make([]byte, maxTemplateReadBytes+1), 0600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	if _, err := readTemplateFile(path); err == nil {
		t.Fatal("expected oversized template to be rejected")
	}
}
