package service

import "testing"

func TestSafeResultPackageRelativePath(t *testing.T) {
	got, err := safeResultPackageRelativePath("organizations/org/workflows/task/attempts/attempt", "organizations/org/workflows/task/attempts/attempt/results/table.parquet")
	if err != nil || got != "results/table.parquet" {
		t.Fatalf("unexpected safe relative path: %q, %v", got, err)
	}
	for _, key := range []string{
		"organizations/org/workflows/task/attempts/attempt/../escape.parquet",
		"organizations/org/workflows/task/attempts/other/table.parquet",
		"organizations/org/workflows/task/attempts/attempt//../escape.parquet",
	} {
		if _, err := safeResultPackageRelativePath("organizations/org/workflows/task/attempts/attempt", key); err == nil {
			t.Fatalf("expected path traversal to be rejected: %s", key)
		}
	}
}

func TestSafeResultPackageRelativePathRejectsPrefixLookalike(t *testing.T) {
	if _, err := safeResultPackageRelativePath("prefix/attempt", "prefix/attempt-other/table.parquet"); err == nil {
		t.Fatal("expected prefix lookalike to be rejected")
	}
}
