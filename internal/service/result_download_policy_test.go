package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestDownloadIssueDeadline(t *testing.T) {
	now := time.Now().UTC()
	paidDeadline := now.Add(3 * time.Hour)
	grant := model.ResultDownload{LinkExpiresAt: &paidDeadline}
	expires, err := downloadIssueDeadline(config.StorageConfig{}, &grant, now)
	if err != nil || expires != now.Add(30*time.Minute) || *grant.LinkExpiresAt != paidDeadline {
		t.Fatalf("initial deadline: %v %v", expires, err)
	}
	soon := now.Add(10 * time.Second)
	grant.LinkExpiresAt = &soon
	expires, err = downloadIssueDeadline(config.StorageConfig{}, &grant, now)
	if err != nil || expires != soon {
		t.Fatalf("link exceeded grant: %v %v", expires, err)
	}
	for _, test := range []struct {
		name  string
		grant model.ResultDownload
	}{
		{"expired", model.ResultDownload{LinkExpiresAt: &now}},
		{"missing", model.ResultDownload{}},
		{"refunded", model.ResultDownload{LinkExpiresAt: &paidDeadline, RefundedAt: &now}},
		{"exhausted", model.ResultDownload{LinkExpiresAt: &paidDeadline, IssueCount: 12}},
		{"too soon", model.ResultDownload{LinkExpiresAt: &paidDeadline, LastIssuedAt: &now}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := downloadIssueDeadline(config.StorageConfig{}, &test.grant, now); err == nil {
				t.Fatal("invalid issuance accepted")
			}
		})
	}
	previous := now.Add(-time.Minute)
	grant = model.ResultDownload{LinkExpiresAt: &paidDeadline, LastIssuedAt: &previous, IssueCount: 11}
	if _, err := downloadIssueDeadline(config.StorageConfig{}, &grant, now); err != nil {
		t.Fatalf("last allowed issue: %v", err)
	}
}

func TestDownloadPauseMarker(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "paused")
	cfg := config.StorageConfig{ResultDownloadPauseFile: marker}
	if err := downloadSigningAvailable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("abnormal egress"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := downloadSigningAvailable(cfg); err == nil {
		t.Fatal("pause ignored")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := downloadSigningAvailable(cfg); err != nil {
		t.Fatalf("resume: %v", err)
	}
}
