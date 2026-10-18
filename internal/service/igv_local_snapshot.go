package service

import (
	"crypto/sha256"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/model"
	"net/url"
	"os"
	"path/filepath"
)

func (s *ResultService) localSnapshotPath(task *model.Task, locus string) (string, string, *igvArchive, error) {
	locus, err := normalizeSnapshotLocus(locus)
	if err != nil {
		return "", "", nil, err
	}
	archive, err := s.loadLocalIGVArchive(task)
	if err != nil {
		return "", "", nil, err
	}
	if s.cfg.ResultQuery.CacheDir == "" {
		return "", "", nil, fmt.Errorf("IGV cache unavailable")
	}
	identity := snapshotIdentity(task, archive.reference.ID, locus)
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(identity+archive.version)))
	return filepath.Join(s.cfg.ResultQuery.CacheDir, "igv-snapshots", key+".png"), locus, archive, nil
}

func (s *ResultService) getLocalIGVSnapshot(task *model.Task, locus string) (*IGVSnapshotResponse, error) {
	filename, locus, archive, err := s.localSnapshotPath(task, locus)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return &IGVSnapshotResponse{Available: false}, nil
	}
	if err != nil {
		return nil, err
	}
	query := url.Values{"locus": {locus}, "attempt": {executionAttempt(task)}, "version": {archive.version}}
	return &IGVSnapshotResponse{Transport: "api", Available: true, URL: "/v1/tasks/" + url.PathEscape(task.UUID) + "/results/igv/snapshot/file?" + query.Encode(), Locus: locus, Reference: archive.reference.ID, CreatedAt: info.ModTime().UTC()}, nil
}

func (s *ResultService) saveLocalIGVSnapshot(task *model.Task, locus, version string, data []byte) (*IGVSnapshotResponse, error) {
	if err := validateSnapshotPNG(data); err != nil {
		return nil, err
	}
	filename, locus, archive, err := s.localSnapshotPath(task, locus)
	if err != nil {
		return nil, err
	}
	if archive.version != version {
		return nil, ErrIGVEvidenceChanged
	}
	hasBAM := false
	for _, candidate := range archive.candidates {
		if candidate.descriptor.Format == "bam" && candidate.descriptor.Available {
			hasBAM = true
		}
	}
	if !hasBAM || !archive.reference.Available {
		return nil, fmt.Errorf("BAM evidence unavailable; cannot create screenshot")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filename); err == nil {
		return s.getLocalIGVSnapshot(task, locus)
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".snapshot-*.png")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return nil, fmt.Errorf("snapshot write failed")
	}
	if err := os.Rename(f.Name(), filename); err != nil {
		return nil, err
	}
	return s.getLocalIGVSnapshot(task, locus)
}

func (s *ResultService) LocalIGVSnapshotFile(task *model.Task, locus, attempt, version string) (string, error) {
	if !s.localBrowserStorage() {
		return "", fmt.Errorf("local IGV unavailable")
	}
	if attempt != executionAttempt(task) {
		return "", ErrIGVEvidenceChanged
	}
	filename, _, archive, err := s.localSnapshotPath(task, locus)
	if err != nil {
		return "", err
	}
	if version != archive.version {
		return "", ErrIGVEvidenceChanged
	}
	return resolveArchiveRegularFile(filepath.Join(s.cfg.ResultQuery.CacheDir, "igv-snapshots"), filename)
}
