package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeParquetPage(t *testing.T) {
	offset, limit := normalizeParquetPage(10_000, 5, MaxParquetPageLimit+1)
	if offset != 5 || limit != 100 {
		t.Fatalf("expected default limit clamp, got offset=%d limit=%d", offset, limit)
	}

	offset, limit = normalizeParquetPage(10, 20, 50)
	if offset != 20 || limit != 0 {
		t.Fatalf("expected out-of-range offset to return empty page, got offset=%d limit=%d", offset, limit)
	}

	offset, limit = normalizeParquetPage(10, 8, 50)
	if offset != 8 || limit != 2 {
		t.Fatalf("expected tail page clamp, got offset=%d limit=%d", offset, limit)
	}
}

func TestResolveParquetRegularFileRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.parquet")
	if err := os.WriteFile(outside, []byte("not really parquet"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(base, "escape.parquet")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	if _, err := resolveParquetRegularFile(base, link); err == nil {
		t.Fatal("expected symlink escaping parquet directory to be rejected")
	}
}

func TestResolveParquetRegularFileAcceptsRegularFile(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "table.parquet")
	if err := os.WriteFile(path, []byte("not really parquet"), 0600); err != nil {
		t.Fatalf("write parquet file: %v", err)
	}
	if got, err := resolveParquetRegularFile(base, path); err != nil || got == "" {
		t.Fatalf("expected regular parquet path to be accepted, got path=%q err=%v", got, err)
	}
}
