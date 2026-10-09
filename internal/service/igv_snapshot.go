package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxIGVSnapshotBytes = 8 << 20

var snapshotLocusPattern = regexp.MustCompile(`^(?:chr)?([1-9]|1[0-9]|2[0-2]|X|Y|M|MT):([0-9]+)-([0-9]+)$`)

type IGVSnapshotResponse struct {
	Available bool      `json:"available"`
	URL       string    `json:"url,omitempty"`
	Locus     string    `json:"locus,omitempty"`
	Reference string    `json:"reference,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

func normalizeSnapshotLocus(locus string) (string, error) {
	m := snapshotLocusPattern.FindStringSubmatch(locus)
	if m == nil {
		return "", fmt.Errorf("invalid locus")
	}
	start, e1 := strconv.ParseInt(m[2], 10, 64)
	end, e2 := strconv.ParseInt(m[3], 10, 64)
	if e1 != nil || e2 != nil || start < 1 || end < start || end > 1_000_000_000 || end-start > 5_000_000 {
		return "", fmt.Errorf("invalid interval")
	}
	chrom := m[1]
	if chrom == "MT" {
		chrom = "M"
	}
	return fmt.Sprintf("chr%s:%d-%d", chrom, start, end), nil
}

func snapshotIdentity(task *model.Task, reference, locus string) string {
	value := strings.Join([]string{model.TenantIDForTask(task), task.UUID, executionAttempt(task), reference, locus}, "\x00")
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func validateSnapshotPNG(data []byte) error {
	if len(data) == 0 || len(data) > MaxIGVSnapshotBytes {
		return fmt.Errorf("screenshot exceeds size limit")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return fmt.Errorf("invalid screenshot dimensions")
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("invalid PNG")
	}
	return nil
}

func (s *ResultService) snapshotScope(task *model.Task, locus string) (string, string, error) {
	if task == nil || s.cfg.Storage.Provider != "s3" {
		return "", "", fmt.Errorf("object storage unavailable")
	}
	for _, value := range []string{task.UUID, task.ExternalOrgID, executionAttempt(task)} {
		if _, err := uuid.Parse(value); err != nil {
			return "", "", fmt.Errorf("invalid task scope")
		}
	}
	normalized, err := normalizeSnapshotLocus(locus)
	return normalized, igvReferenceForTask(s.cfg, task).ID, err
}

func (s *ResultService) findIGVSnapshot(ctx context.Context, task *model.Task, reference, locus string) (*model.IGVSnapshot, error) {
	var row model.IGVSnapshot
	err := database.GetDB().WithContext(ctx).Where("identity = ? AND tenant_id = ? AND task_uuid = ? AND execution_attempt_id = ?", snapshotIdentity(task, reference, locus), model.TenantIDForTask(task), task.UUID, executionAttempt(task)).First(&row).Error
	return &row, err
}

func (s *ResultService) GetIGVSnapshot(ctx context.Context, task *model.Task, locus string) (*IGVSnapshotResponse, error) {
	locus, reference, err := s.snapshotScope(task, locus)
	if err != nil {
		return nil, err
	}
	row, err := s.findIGVSnapshot(ctx, task, reference, locus)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &IGVSnapshotResponse{Available: false}, nil
	}
	if err != nil {
		return nil, err
	}
	store, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	url, err := store.presignRead(ctx, row.ObjectKey, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	return &IGVSnapshotResponse{Available: true, URL: url, Locus: row.Locus, Reference: row.Reference, CreatedAt: row.CreatedAt}, nil
}

func (s *ResultService) SaveIGVSnapshot(ctx context.Context, task *model.Task, locus, version string, data []byte) (*IGVSnapshotResponse, error) {
	locus, reference, err := s.snapshotScope(task, locus)
	if err != nil {
		return nil, err
	}
	if err := validateSnapshotPNG(data); err != nil {
		return nil, err
	}
	archive, err := s.loadIGVArchive(ctx, task)
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
	if _, err := s.findIGVSnapshot(ctx, task, reference, locus); err == nil {
		return s.GetIGVSnapshot(ctx, task, locus)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row := model.IGVSnapshot{ID: uuid.NewString(), Identity: snapshotIdentity(task, reference, locus), TaskUUID: task.UUID, ExecutionAttemptID: executionAttempt(task), TenantID: model.TenantIDForTask(task), Reference: reference, Locus: locus, EvidenceVersion: version, CreatedAt: time.Now().UTC()}
	resolved := *task
	resolved.ExecutionAttemptID = executionAttempt(task)
	row.ObjectKey = path.Join(resultPackagePrefix(&resolved), "_igv-snapshots", row.ID+".png")
	if err := archive.storage.put(ctx, row.ObjectKey, "image/png", data); err != nil {
		return nil, err
	}
	result := database.GetDB().WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if result.Error != nil || result.RowsAffected == 0 {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = archive.storage.delete(cleanup, row.ObjectKey)
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return s.GetIGVSnapshot(ctx, task, locus)
}
