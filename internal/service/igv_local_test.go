package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"image"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func localIGVFixture(t *testing.T) (*ResultService, *model.Task, string) {
	t.Helper()
	root := t.TempDir()
	task := &model.Task{UUID: "11111111-1111-4111-8111-111111111111", ExecutionAttemptID: "22222222-2222-4222-8222-222222222222", InputJSON: `{"SingleWES.fasta":"/untrusted/GRCh37.fa"}`}
	dir := filepath.Join(root, task.UUID)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"sample.bam": "BAM0123456789", "sample.bam.bai": "BAI0123", "unlisted.bam": "private", "reference.fa": ">1\nACGTACGT\n", "reference.fa.fai": "1\t8\t3\t8\t9\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]string{"bam": "/original/run/sample.bam", "bai": "/original/run/sample.bam.bai"})
	if err := os.WriteFile(filepath.Join(dir, "outputs.resolved.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Edition: "opensource", Storage: config.StorageConfig{Provider: "local"}, Task: config.TaskConfig{ArchiveDir: root}, ResultQuery: config.ResultQueryConfig{CacheDir: filepath.Join(root, "cache")}, IGV: config.IGVConfig{HG19: config.IGVReferenceConfig{FASTAPath: filepath.Join(dir, "reference.fa")}}}
	return NewResultService(cfg), task, dir
}

func TestLocalIGVDiscoveryAuthorizationAndRanges(t *testing.T) {
	s, task, dir := localIGVFixture(t)
	session, err := s.GetIGVSession(context.Background(), task)
	if err != nil || !session.Available || session.Transport != "api" || len(session.Tracks) != 1 || !session.Tracks[0].HasIndex {
		t.Fatalf("invalid local session: %+v %v", session, err)
	}
	if strings.Contains(session.Reference.FASTAURL, dir) || !strings.Contains(session.Reference.FASTAURL, "/igv/reference/fasta?") {
		t.Fatal("filesystem path leaked")
	}
	urls, err := s.SignIGVTracks(context.Background(), task, model.IGVURLRequest{Version: session.Version, TrackIDs: []string{"bam:0"}})
	if err != nil || urls.Transport != "api" || !strings.Contains(urls.Tracks[0].URL, "bam:0/data?") {
		t.Fatalf("local track URLs: %+v %v", urls, err)
	}
	stream, err := s.OpenLocalIGVTrack(task, "bam:0", "data", executionAttempt(task), session.Version, "bytes=3-6")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(stream.Body)
	stream.Body.Close()
	if string(data) != "0123" || stream.ContentRange != "bytes 3-6/13" {
		t.Fatalf("range was not preserved: %q %s", data, stream.ContentRange)
	}
	for _, value := range []string{"", "bytes=0-999999999", "bytes=-3", "bytes=1-2,4-5"} {
		// An end beyond EOF is valid and clipped, per HTTP range semantics.
		if value == "bytes=0-999999999" {
			continue
		}
		if _, err := s.OpenLocalIGVTrack(task, "bam:0", "data", executionAttempt(task), session.Version, value); !errors.Is(err, ErrIGVReferenceRange) {
			t.Fatalf("invalid range accepted: %q %v", value, err)
		}
	}
	if _, err := s.OpenLocalIGVTrack(task, "bam:0", "data", "old", session.Version, "bytes=0-3"); !errors.Is(err, ErrIGVEvidenceChanged) {
		t.Fatal("stale attempt accepted")
	}
	if _, err := s.OpenLocalIGVTrack(task, "bam:0", "data", executionAttempt(task), "old", "bytes=0-3"); !errors.Is(err, ErrIGVEvidenceChanged) {
		t.Fatal("stale version accepted")
	}
	if _, err := s.OpenLocalIGVTrack(task, "unlisted.bam", "data", executionAttempt(task), session.Version, "bytes=0-3"); err == nil {
		t.Fatal("arbitrary filename accepted")
	}
	stream, err = s.OpenIGVReference(context.Background(), task, "fasta", executionAttempt(task), "bytes=3-6")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(stream.Body)
	stream.Body.Close()
	if string(data) != "ACGT" {
		t.Fatal("reference did not use administrator-configured file")
	}
	s.cfg.IGV.HG19.FASTAPath = ""
	if _, err := s.OpenIGVReference(context.Background(), task, "fasta", executionAttempt(task), "bytes=0-3"); err == nil {
		t.Fatal("untrusted input path accepted")
	}
}

func TestLocalVCFReferenceContextAndIGVAgree(t *testing.T) {
	s, task, dir := localIGVFixture(t)
	task.Template = "vcf2anno"
	for _, inputs := range []string{`{"VCF2Anno.assembly":"GRCh37"}`, `{}`} {
		task.InputJSON = inputs
		task.PipelineSnapshotJSON = `{"reference":"hg19"}`
		ref := resultReferenceForTask(s.cfg, task)
		if !ref.Available || ref.DeclaredID != "hg19" || ref.Reason != "" {
			t.Fatalf("local context ignored configured FASTA/FAI: %+v", ref)
		}
		if !s.localIGVReference(task).Available {
			t.Fatal("context and IGV disagreed")
		}
	}
	s.cfg.IGV.HG38.FASTAPath = filepath.Join(dir, "reference.fa")
	task.InputJSON = `{"VCF2Anno.assembly":"GRCh38"}`
	if ref := resultReferenceForTask(s.cfg, task); !ref.Available || ref.DeclaredID != "hg38" {
		t.Fatalf("hg38 execution used a different pipeline reference: %+v", ref)
	}
	if err := os.Remove(filepath.Join(dir, "reference.fa.fai")); err != nil {
		t.Fatal(err)
	}
	if ref := resultReferenceForTask(s.cfg, task); ref.Available {
		t.Fatal("missing FAI was reported as available")
	}
	task.InputJSON = `{"VCF2Anno.assembly":"T2T-CHM13"}`
	if _, err := s.localIGVReferencePath(task); err == nil {
		t.Fatal("unsupported assembly fell back to hg19")
	}
}

func TestLocalIGVRejectsEscapingSymlinksAndSaaS(t *testing.T) {
	s, task, dir := localIGVFixture(t)
	outside := filepath.Join(t.TempDir(), "private.bam")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "sample.bam")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "sample.bam")); err != nil {
		t.Skip("symlink unavailable")
	}
	session, err := s.GetIGVSession(context.Background(), task)
	if err != nil || session.Available || session.Tracks[0].Available {
		t.Fatal("outside symlink exposed as evidence")
	}
	s.cfg.Edition = "saas"
	if _, err := s.OpenLocalIGVTrack(task, "bam:0", "data", executionAttempt(task), session.Version, "bytes=0-3"); err == nil {
		t.Fatal("SaaS used local transport")
	}
}

func TestLocalIGVSnapshotLifecycle(t *testing.T) {
	s, task, _ := localIGVFixture(t)
	ctx := context.Background()
	initial, err := s.GetIGVSnapshot(ctx, task, "chr1:1-20")
	if err != nil || initial.Available {
		t.Fatal("initial snapshot lookup failed", err)
	}
	session, _ := s.GetIGVSession(ctx, task)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	saved, err := s.SaveIGVSnapshot(ctx, task, "chr1:1-20", session.Version, imageData.Bytes())
	if err != nil || !saved.Available || saved.Transport != "api" {
		t.Fatal("snapshot save failed", err)
	}
	u, _ := url.Parse(saved.URL)
	filename, err := s.LocalIGVSnapshotFile(task, u.Query().Get("locus"), u.Query().Get("attempt"), u.Query().Get("version"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(data, imageData.Bytes()) {
		t.Fatal("snapshot content changed")
	}
	if _, err := s.LocalIGVSnapshotFile(task, "chr1:1-20", "old", session.Version); !errors.Is(err, ErrIGVEvidenceChanged) {
		t.Fatal("stale snapshot accepted")
	}
	if _, err := s.SaveIGVSnapshot(ctx, task, "chr1:1-20", "old", imageData.Bytes()); !errors.Is(err, ErrIGVEvidenceChanged) {
		t.Fatal("stale screenshot save accepted")
	}
}
