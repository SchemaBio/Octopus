package service

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Full validation streams both files without loading sequencing data into RAM.
// Bound concurrent scans so a batch cannot exhaust disk bandwidth or memory.
var localImportSlots = make(chan struct{}, 2)

func localImportError(code string) error { return errors.New("local_import_" + code) }

type localFASTQ struct {
	file    *os.File
	before  os.FileInfo
	scanner *bufio.Scanner
	path    string
	digest  hashWriter
	gzip    *gzip.Reader
	read    int
}
type hashWriter interface {
	io.Writer
	Sum([]byte) []byte
}

func openLocalFASTQ(root, path string) (*localFASTQ, error) {
	if !filepath.IsAbs(path) {
		return nil, localImportError("absolute_path")
	}
	resolved, err := safeLocalUploadPath(root, path)
	if err != nil {
		return nil, localImportError("path")
	}
	name := strings.ToLower(resolved)
	if !strings.HasSuffix(name, ".fastq") && !strings.HasSuffix(name, ".fq") && !strings.HasSuffix(name, ".fastq.gz") && !strings.HasSuffix(name, ".fq.gz") {
		return nil, localImportError("format")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, localImportError("path")
	}
	before, err := file.Stat()
	if err != nil || before.Size() == 0 {
		file.Close()
		return nil, localImportError("empty")
	}
	digest := sha256.New()
	var reader io.Reader = io.TeeReader(file, digest)
	var compressed *gzip.Reader
	if strings.HasSuffix(name, ".gz") {
		compressed, err = gzip.NewReader(reader)
		if err != nil {
			file.Close()
			return nil, localImportError("integrity")
		}
		reader = compressed
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	return &localFASTQ{file: file, before: before, scanner: scanner, path: resolved, digest: digest, gzip: compressed}, nil
}
func (f *localFASTQ) close() {
	if f.gzip != nil {
		f.gzip.Close()
	}
	f.file.Close()
}
func readID(header string) string {
	fields := strings.Fields(header)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSuffix(fields[0], "/1"), "/2")
}
func (f *localFASTQ) next() (string, bool, error) {
	if !f.scanner.Scan() {
		if f.scanner.Err() != nil {
			return "", false, localImportError("integrity")
		}
		return "", false, nil
	}
	header := f.scanner.Text()
	if len(header) < 2 || header[0] != '@' {
		return "", false, localImportError("format")
	}
	lines := [3]string{}
	for i := range lines {
		if !f.scanner.Scan() {
			return "", false, localImportError("integrity")
		}
		lines[i] = f.scanner.Text()
	}
	if len(lines[0]) == 0 || len(lines[1]) == 0 || lines[1][0] != '+' || len(lines[0]) != len(lines[2]) {
		return "", false, localImportError("format")
	}
	for _, q := range []byte(lines[2]) {
		if q < 33 || q > 126 {
			return "", false, localImportError("format")
		}
	}
	parts := strings.Fields(header[1:])
	if len(parts) > 0 {
		if (strings.HasSuffix(parts[0], "/1") && f.read == 2) || (strings.HasSuffix(parts[0], "/2") && f.read == 1) {
			return "", false, localImportError("pair")
		}
		if len(parts) > 1 && ((strings.HasPrefix(parts[1], "1:") && f.read == 2) || (strings.HasPrefix(parts[1], "2:") && f.read == 1)) {
			return "", false, localImportError("pair")
		}
	}
	for _, base := range []byte(lines[0]) {
		if !strings.ContainsRune("ACGTUNRYSWKMBDHVacgtunryswkmbdhv", rune(base)) {
			return "", false, localImportError("format")
		}
	}
	id := readID(header[1:])
	if id == "" {
		return "", false, localImportError("format")
	}
	return id, true, nil
}
func (f *localFASTQ) unchanged(root string) bool {
	resolved, err := safeLocalUploadPath(root, f.path)
	if err != nil {
		return false
	}
	now, err := os.Stat(resolved)
	return err == nil && os.SameFile(f.before, now) && now.Size() == f.before.Size() && now.ModTime().Equal(f.before.ModTime())
}

func validateLocalPair(ctx context.Context, root, read1, read2 string) ([]model.DataAsset, error) {
	first, err := openLocalFASTQ(root, read1)
	if err != nil {
		return nil, err
	}
	defer first.close()
	second, err := openLocalFASTQ(root, read2)
	if err != nil {
		return nil, err
	}
	defer second.close()
	if os.SameFile(first.before, second.before) {
		return nil, localImportError("same_file")
	}
	first.read = 1
	second.read = 2
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, localImportError("cancelled")
		}
		id1, ok1, err := first.next()
		if err != nil {
			return nil, err
		}
		id2, ok2, err := second.next()
		if err != nil {
			return nil, err
		}
		if ok1 != ok2 {
			return nil, localImportError("pair")
		}
		if !ok1 {
			break
		}
		if id1 != id2 {
			return nil, localImportError("pair")
		}
		count++
	}
	if count == 0 {
		return nil, localImportError("empty")
	}
	if !first.unchanged(root) || !second.unchanged(root) {
		return nil, localImportError("changed")
	}
	now := time.Now().UTC()
	result := make([]model.DataAsset, 2)
	for i, file := range []*localFASTQ{first, second} {
		readType := model.ReadTypeRead1
		if i == 1 {
			readType = model.ReadTypeRead2
		}
		result[i] = model.DataAsset{UUID: uuid.NewString(), Provider: model.UploadProviderLocal, StorageKey: file.path, FileName: filepath.Base(file.path), FileSize: file.before.Size(), ReadType: readType, Status: model.FileStatusCompleted, Source: model.DataAssetSourceLocalImport, ValidationStatus: "valid", ValidationSHA256: hex.EncodeToString(file.digest.Sum(nil)), ValidatedAt: &now}
	}
	return result, nil
}

func (s *DataAssetService) ImportLocalPair(ctx context.Context, req *model.LocalDataImportRequest, actor model.OverlayActor) ([]model.DataAssetResponse, error) {
	if s.cfg == nil || s.cfg.Edition != "opensource" || s.cfg.Storage.Provider != "local" || !actor.SharedInstance || actor.UserID == 0 {
		return nil, localImportError("disabled")
	}
	internalID := strings.TrimSpace(req.InternalID)
	if internalID == "" || len([]rune(internalID)) > 100 {
		return nil, localImportError("internal_id")
	}
	select {
	case localImportSlots <- struct{}{}:
		defer func() { <-localImportSlots }()
	default:
		return nil, localImportError("busy")
	}
	assets, err := validateLocalPair(ctx, s.cfg.Storage.LocalDir, strings.TrimSpace(req.Read1Path), strings.TrimSpace(req.Read2Path))
	if err != nil {
		return nil, err
	}
	for i := range assets {
		assets[i].CreatedBy = actor.UserID
		assets[i].InternalID = internalID
	}
	err = database.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Revocation during a long integrity scan must still prevent registration.
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&user, actor.UserID).Error; err != nil {
			return localImportError("permission_denied")
		}
		if !user.IsActive || !model.HasPermission(user.EffectivePermissions(), "data.manage") || !model.HasPermission(user.EffectivePermissions(), "samples.write") {
			return localImportError("permission_denied")
		}
		for _, asset := range assets {
			var count int64
			if err := tx.Model(&model.DataAsset{}).Where("provider=? AND storage_key=?", model.UploadProviderLocal, asset.StorageKey).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return localImportError("duplicate")
			}
		}
		if err := tx.Create(&assets).Error; err != nil {
			// The unique provider/key index also protects concurrent requests.
			if strings.Contains(err.Error(), "23505") || errors.Is(err, gorm.ErrDuplicatedKey) {
				return localImportError("duplicate")
			}
			return err
		}
		return nil
	})
	if err != nil {
		if strings.HasPrefix(err.Error(), "local_import_") {
			return nil, err
		}
		return nil, localImportError("database")
	}
	responses := make([]model.DataAssetResponse, len(assets))
	for i := range assets {
		responses[i] = model.DataAssetToResponse(&assets[i])
	}
	return responses, nil
}
