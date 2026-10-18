package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func localDownloadFixture(t *testing.T) (*ResultDownloadService, *model.Task, string) {
	t.Helper()
	s, task, dir := localIGVFixture(t)
	task.Status = model.TaskStatusCompleted
	task.ResultImportStatus = model.ResultImportStatusSuccess
	for name, content := range map[string]string{"sample.snv_indel.parquet": "PAR1result", "sample.vcf.gz": "compressed vcf", "private.env": "not a result"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{"summary": map[string]interface{}{"bam": "sample.bam", "bai": "sample.bam.bai", "snp_indel": "sample.snv_indel.parquet", "vcf_raw": "sample.vcf.gz", "qc_result": map[string]interface{}{"quality": "test"}}})
	if err := os.WriteFile(filepath.Join(dir, "outputs.resolved.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return &ResultDownloadService{cfg: s.cfg}, task, dir
}

func TestLocalDownloadCatalogZIPAndFileScope(t *testing.T) {
	s, task, dir := localDownloadFixture(t)
	ctx := context.Background()
	catalog, err := s.Catalog(ctx, task, false)
	if err != nil || catalog.Transport != "local" || catalog.ZIPStatus != "pending" || len(catalog.Files) != 4 || len(catalog.BAMs) != 1 {
		t.Fatalf("invalid local catalog: %+v %v", catalog, err)
	}
	for _, file := range catalog.Files {
		if file.Credits != 0 || strings.Contains(file.Filename, dir) {
			t.Fatal("billing or filesystem path exposed")
		}
	}
	if _, err := s.Quote(ctx, task, model.OverlayActor{}, "", "bam", catalog.BAMs[0].ID); err == nil {
		t.Fatal("local download entered billing quote flow")
	}
	if _, err := s.Issue(ctx, task, model.OverlayActor{}, "", "old-grant"); err == nil {
		t.Fatal("local download entered billing issuance flow")
	}
	filename, name, redirect, err := s.SelfHostedDownloadFile(ctx, task, catalog.BAMs[0].ID, catalog.AttemptID, catalog.Version)
	if err != nil || filename != filepath.Join(dir, "sample.bam") || name != "sample.bam" || redirect != "" {
		t.Fatal("local BAM resolution failed", err)
	}
	if _, _, _, err := s.SelfHostedDownloadFile(ctx, task, downloadID(filepath.Join(dir, "private.env")), catalog.AttemptID, catalog.Version); err == nil {
		t.Fatal("unlisted file was exposed")
	}
	if _, _, _, err := s.SelfHostedDownloadFile(ctx, task, catalog.BAMs[0].ID, "old", catalog.Version); !errors.Is(err, ErrIGVEvidenceChanged) {
		t.Fatal("stale attempt accepted")
	}
	if _, _, _, err := s.SelfHostedDownloadFile(ctx, task, catalog.BAMs[0].ID, catalog.AttemptID, "old"); !errors.Is(err, ErrIGVEvidenceChanged) {
		t.Fatal("stale manifest accepted")
	}
	archive, err := s.collectLocalDownloads(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.writeLocalDownloadZIP(ctx, task, archive); err != nil {
		t.Fatal(err)
	}
	catalog, err = s.Catalog(ctx, task, false)
	if err != nil || catalog.ZIPStatus != "ready" || catalog.ZIP == nil || catalog.ZIP.Credits != 0 {
		t.Fatal("ZIP not reusable", err)
	}
	filename, _, _, err = s.SelfHostedDownloadFile(ctx, task, catalog.ZIP.ID, catalog.AttemptID, catalog.Version)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, file := range zr.File {
		names[file.Name] = true
		if strings.Contains(file.Name, "/") || strings.HasSuffix(file.Name, ".bam") || strings.HasSuffix(file.Name, ".bai") || file.Name == "private.env" || file.Name == "outputs.resolved.json" {
			t.Fatal("ZIP contains unintended content")
		}
	}
	if !names["sample.snv_indel.parquet"] || !names["sample.vcf.gz"] || !names["qc-result.json"] {
		t.Fatal("missing ZIP outputs")
	}
	other := *task
	other.UUID = "33333333-3333-4333-8333-333333333333"
	if _, _, _, err := s.SelfHostedDownloadFile(ctx, &other, catalog.BAMs[0].ID, executionAttempt(&other), catalog.Version); err == nil {
		t.Fatal("cross-task resource accepted")
	}
	s.cfg.Edition = "saas"
	if _, _, _, err := s.SelfHostedDownloadFile(ctx, task, catalog.BAMs[0].ID, catalog.AttemptID, catalog.Version); !errors.Is(err, ErrResultPackageUnsupported) {
		t.Fatal("SaaS entered self-hosted file transport")
	}
}

func TestLocalDownloadPrepareAndLimits(t *testing.T) {
	s, task, dir := localDownloadFixture(t)
	ctx := context.Background()
	task.ResultImportStatus = model.ResultImportStatusPending
	if _, err := s.Catalog(ctx, task, false); err == nil {
		t.Fatal("archive still being copied was exposed")
	}
	task.ResultImportStatus = model.ResultImportStatusSuccess
	catalog, err := s.Catalog(ctx, task, true)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for catalog.ZIPStatus != "ready" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		catalog, err = s.Catalog(ctx, task, false)
		if err != nil {
			t.Fatal(err)
		}
	}
	if catalog.ZIPStatus != "ready" {
		t.Fatal("asynchronous preparation did not finish", catalog.ZIPError)
	}
	s.cfg.Report.PackageMaxSizeMB = 1
	if err := os.WriteFile(filepath.Join(dir, "sample.snv_indel.parquet"), make([]byte, 2<<20), 0600); err != nil {
		t.Fatal(err)
	}
	archive, err := s.collectLocalDownloads(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.writeLocalDownloadZIP(ctx, task, archive); err == nil {
		t.Fatal("source size limit bypassed")
	}
	if _, err := os.Stat(archive.zipPath); !os.IsNotExist(err) {
		t.Fatal("failed package published")
	}
	if _, _, _, err := s.SelfHostedDownloadFile(ctx, task, catalog.ZIP.ID, catalog.AttemptID, catalog.Version); !errors.Is(err, ErrIGVEvidenceChanged) {
		t.Fatal("changed source accepted by old URL")
	}
}

func TestLocalDownloadRejectsChangedSourcesAndEscapingLinks(t *testing.T) {
	s, task, dir := localDownloadFixture(t)
	archive, err := s.collectLocalDownloads(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample.snv_indel.parquet"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.writeLocalDownloadZIP(context.Background(), task, archive); err == nil {
		t.Fatal("source change not detected")
	}
	if _, err := os.Stat(archive.zipPath); !os.IsNotExist(err) {
		t.Fatal("partial archive exposed")
	}
	outside := filepath.Join(t.TempDir(), "outside.bam")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "sample.bam")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "sample.bam")); err != nil {
		t.Skip("symlink unavailable")
	}
	catalog, err := s.Catalog(context.Background(), task, false)
	if err != nil || len(catalog.BAMs) != 0 || len(catalog.Missing) == 0 {
		t.Fatal("outside symlink was not excluded")
	}
}
