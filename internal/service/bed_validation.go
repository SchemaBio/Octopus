package service

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

var bedValidationSlots = make(chan struct{}, 2)
var errBEDContent = errors.New("invalid BED content")

func bedContig(s string) string {
	s = strings.TrimPrefix(s, "chr")
	if s == "M" {
		return "MT"
	}
	return s
}
func parseBEDIndex(r io.Reader) (map[string]int64, error) {
	data, e := io.ReadAll(io.LimitReader(r, (4<<20)+1))
	if e != nil || len(data) > 4<<20 {
		return nil, errors.New("reference index unavailable or oversized")
	}
	result := map[string]int64{}
	scan := bufio.NewScanner(strings.NewReader(string(data)))
	scan.Buffer(make([]byte, 4096), 65536)
	for scan.Scan() {
		f := strings.Fields(scan.Text())
		if len(f) < 2 {
			return nil, errors.New("invalid reference index")
		}
		n, e := strconv.ParseInt(f[1], 10, 64)
		if e != nil || n <= 0 {
			return nil, errors.New("invalid reference index")
		}
		key := bedContig(f[0])
		if old, ok := result[key]; ok && old != n {
			return nil, errors.New("ambiguous reference contig")
		}
		result[key] = n
	}
	if scan.Err() != nil || len(result) == 0 {
		return nil, errors.New("reference index unavailable")
	}
	return result, nil
}

func validateBEDStream(ctx context.Context, r io.Reader, compressed bool, contigs map[string]int64) error {
	if compressed {
		g, e := gzip.NewReader(r)
		if e != nil {
			return fmt.Errorf("%w: gzip header", errBEDContent)
		}
		defer g.Close()
		r = g
	}
	limited := &io.LimitedReader{R: r, N: (256 << 20) + 1}
	scan := bufio.NewScanner(limited)
	scan.Buffer(make([]byte, 4096), 1<<20)
	intervals := 0
	lines := 0
	for scan.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lines++
		if lines > 2000000 {
			return errors.New("BED validation resource limit")
		}
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") || line == "track" || strings.HasPrefix(line, "track ") || line == "browser" || strings.HasPrefix(line, "browser ") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			return fmt.Errorf("%w: columns", errBEDContent)
		}
		start, e1 := strconv.ParseInt(f[1], 10, 64)
		end, e2 := strconv.ParseInt(f[2], 10, 64)
		length, exists := contigs[bedContig(f[0])]
		if e1 != nil || e2 != nil || start < 0 || end < start || !exists || end > length {
			return fmt.Errorf("%w: coordinates or contig", errBEDContent)
		}
		intervals++
	}
	if limited.N <= 0 {
		return errors.New("BED validation resource limit")
	}
	if e := scan.Err(); e != nil {
		if errors.Is(e, gzip.ErrChecksum) || errors.Is(e, gzip.ErrHeader) || errors.Is(e, io.ErrUnexpectedEOF) {
			return fmt.Errorf("%w: gzip integrity", errBEDContent)
		}
		return e
	}
	if intervals == 0 {
		return fmt.Errorf("%w: no intervals", errBEDContent)
	}
	return nil
}

func (s *DataAssetService) ValidateBED(ctx context.Context, id string, a model.OverlayActor) (*model.DataAsset, error) {
	asset, e := s.Get(id, a)
	if e != nil {
		return nil, e
	}
	if asset.ReadType != model.ReadTypeBed || asset.Status != model.FileStatusCompleted {
		return nil, errors.New("completed BED required")
	}
	select {
	case bedValidationSlots <- struct{}{}:
		defer func() { <-bedValidationSlots }()
	default:
		return nil, errors.New("BED validation busy; retry shortly")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	claim := database.GetDB().WithContext(ctx).Model(&model.DataAsset{}).Where("id=? AND status=? AND (validation_status<>'validating' OR updated_at<?)", asset.ID, model.FileStatusCompleted, time.Now().Add(-time.Minute)).Updates(map[string]interface{}{"validation_status": "validating", "validation_code": ""})
	if claim.Error != nil {
		return nil, claim.Error
	}
	if claim.RowsAffected != 1 {
		return nil, errors.New("BED validation already running")
	}
	code, status, hash, refHash := s.checkBED(ctx, asset)
	now := time.Now().UTC()
	if e = database.GetDB().Model(&model.DataAsset{}).Where("id=? AND status=? AND validation_status='validating'", asset.ID, model.FileStatusCompleted).Updates(map[string]interface{}{"validation_status": status, "validation_code": code, "validation_sha256": hash, "validation_reference_sha256": refHash, "validated_at": now}).Error; e != nil {
		return nil, e
	}
	asset.ValidationStatus = status
	asset.ValidationCode = code
	asset.ValidationSHA256 = hash
	asset.ValidationReferenceSHA256 = refHash
	asset.ValidatedAt = &now
	return asset, nil
}

func (s *DataAssetService) checkBED(ctx context.Context, asset *model.DataAsset) (string, string, string, string) {
	unavailable := func(code string) (string, string, string, string) { return code, "unavailable", "", "" }
	cfg, e := cvmReferenceStorageConfig(s.cfg.Storage)
	if e != nil {
		return unavailable("REFERENCE_INDEX_UNAVAILABLE")
	}
	cfg.S3Bucket = cfg.CVMReferenceBucket
	if cfg.S3Bucket == "" {
		cfg.S3Bucket = defaultCVMReferenceBucket
	}
	genome := "hg19"
	if asset.ReferenceGenome == model.ReferenceGenomeGRCh38 {
		genome = "hg38"
	} else if asset.ReferenceGenome != model.ReferenceGenomeGRCh37 {
		return unavailable("REFERENCE_IDENTITY_UNKNOWN")
	}
	keys, _ := cvmReferenceObjectKeys(genome, false)
	storage, e := newS3Storage(ctx, cfg)
	if e != nil {
		return unavailable("REFERENCE_INDEX_UNAVAILABLE")
	}
	ref, e := storage.open(ctx, keys[1])
	if e != nil {
		return unavailable("REFERENCE_INDEX_UNAVAILABLE")
	}
	defer ref.Close()
	refHasher := sha256.New()
	contigs, e := parseBEDIndex(io.TeeReader(ref, refHasher))
	if e != nil {
		return unavailable("REFERENCE_INDEX_UNAVAILABLE")
	}
	var source io.ReadCloser
	if asset.Provider == model.UploadProviderS3 {
		store, e := newS3Storage(ctx, s.cfg.Storage)
		if e != nil {
			return unavailable("BED_OBJECT_UNAVAILABLE")
		}
		source, e = store.open(ctx, asset.StorageKey)
		if e != nil {
			return unavailable("BED_OBJECT_UNAVAILABLE")
		}
	} else {
		path, e := safeLocalUploadPath(s.cfg.Storage.LocalDir, asset.StorageKey)
		if e != nil {
			return unavailable("BED_OBJECT_UNAVAILABLE")
		}
		source, e = os.Open(path)
		if e != nil {
			return unavailable("BED_OBJECT_UNAVAILABLE")
		}
	}
	defer source.Close()
	f, e := os.CreateTemp("", "bed-validation-*")
	if e != nil {
		return unavailable("VALIDATOR_UNAVAILABLE")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(source, (20<<20)+1))
	if e != nil || n != asset.FileSize || n <= 0 || n > 20<<20 {
		return unavailable("BED_OBJECT_SIZE_MISMATCH")
	}
	if _, e = f.Seek(0, 0); e != nil {
		return unavailable("VALIDATOR_UNAVAILABLE")
	}
	hash := hex.EncodeToString(h.Sum(nil))
	refHash := hex.EncodeToString(refHasher.Sum(nil))
	e = validateBEDStream(ctx, f, strings.HasSuffix(strings.ToLower(asset.FileName), ".gz"), contigs)
	if errors.Is(e, errBEDContent) {
		return "BED_CONTENT_INVALID", "invalid", hash, refHash
	}
	if e != nil {
		return unavailable("VALIDATOR_UNAVAILABLE")
	}
	return "", "valid", hash, refHash
}

func scheduleBEDValidation(cfg *config.Config, asset *model.DataAsset) {
	if asset.ReadType != model.ReadTypeBed || asset.Status != model.FileStatusCompleted {
		return
	}
	if asset.ValidationStatus != "" && asset.ValidationStatus != "pending" && !(asset.ValidationStatus == "validating" && asset.UpdatedAt.Before(time.Now().Add(-time.Minute))) {
		return
	}
	copy := *asset
	go func() {
		_, _ = NewDataAssetService(cfg).ValidateBED(context.Background(), copy.UUID, model.OverlayActor{UserID: copy.CreatedBy, OrgID: copy.ExternalOrgID})
	}()
}

func bedUsable(asset *model.DataAsset) bool {
	return asset != nil && asset.Status == model.FileStatusCompleted && asset.ReadType == model.ReadTypeBed && asset.ValidationStatus == "valid" && asset.ValidationSHA256 != ""
}
