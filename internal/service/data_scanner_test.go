package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
)

func TestDataScannerRequiresMetadataForSelfHostedLocalFiles(t *testing.T) {
	root, one, two := localPairFixture(t)
	for old, name := range map[string]string{one: "unregistered_R1.fastq.gz", two: "unregistered_R2.fastq.gz"} {
		if err := os.Rename(old, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Edition: "opensource"}
	cfg.Storage.Provider = "local"
	cfg.Storage.LocalDir = root
	cfg.Storage.ScanLocalDir = root
	// No repository is supplied: inspecting a copied file for registration
	// would fail this test instead of silently creating asset metadata.
	scanner := &DataScanner{cfg: cfg}
	if scanner.Enabled() {
		t.Fatal("self-hosted copied FASTQ files must require metadata registration")
	}
	// Even with a stable complete pair and legacy scanning configured, a scan
	// must perform no database discovery or insertion.
	scanner.scan(context.Background(), 0)
}

func TestDataScannerPreservesOtherDeploymentRules(t *testing.T) {
	for _, scenario := range []struct {
		name, edition, provider, local, prefix string
		enabled                                bool
	}{
		{"saas local configured", "saas", "local", "/inbox", "", true},
		{"saas local unconfigured", "saas", "local", "", "", false},
		{"saas s3 configured", "saas", "s3", "", "inbox", true},
		{"saas s3 unconfigured", "saas", "s3", "", "", false},
		{"self-hosted s3 unchanged", "opensource", "s3", "", "inbox", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := &config.Config{Edition: scenario.edition}
			cfg.Storage.Provider = scenario.provider
			cfg.Storage.ScanLocalDir = scenario.local
			cfg.Storage.S3ScanPrefix = scenario.prefix
			if got := (&DataScanner{cfg: cfg}).Enabled(); got != scenario.enabled {
				t.Fatalf("scanner enabled = %v, want %v", got, scenario.enabled)
			}
		})
	}
}
