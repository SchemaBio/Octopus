package service

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func validateLocalVCF(ctx context.Context, root, filename string) ([]model.DataAsset, error) {
	if !filepath.IsAbs(filename) || !isVCFFilename(filename) {
		return nil, localImportError("format")
	}
	filename, err := safeLocalUploadPath(root, filename)
	if err != nil {
		return nil, localImportError("path")
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, localImportError("path")
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() == 0 {
		return nil, localImportError("empty")
	}
	digest := sha256.New()
	raw := bufio.NewReader(io.TeeReader(f, digest))
	var source io.Reader = raw
	magic, _ := raw.Peek(2)
	if len(magic) == 2 && magic[0] == 31 && magic[1] == 139 {
		gz, err := gzip.NewReader(raw)
		if err != nil {
			return nil, localImportError("integrity")
		}
		defer gz.Close()
		source = gz
	}
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 65536), 16*1024*1024)
	header := false
	columns := 0
	version := false
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, localImportError("cancelled")
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "##fileformat=VCFv4.") {
			version = true
		}
		if strings.HasPrefix(line, "##") {
			continue
		}
		fields := strings.Split(line, "\t")
		if strings.HasPrefix(line, "#CHROM\t") {
			if header || !version || len(fields) < 10 {
				return nil, localImportError("format")
			}
			names := map[string]bool{}
			for _, name := range fields[9:] {
				if name == "" || names[name] {
					return nil, localImportError("format")
				}
				names[name] = true
			}
			header = true
			columns = len(fields)
			continue
		}
		if !header || len(fields) != columns || !strings.Contains(":"+fields[8]+":", ":GT:") {
			return nil, localImportError("format")
		}
	}
	if scanner.Err() != nil {
		return nil, localImportError("integrity")
	}
	if !header {
		return nil, localImportError("format")
	}
	after, err := os.Stat(filename)
	resolved, resolveErr := safeLocalUploadPath(root, filename)
	if err != nil || resolveErr != nil || resolved != filename || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, localImportError("changed")
	}
	return []model.DataAsset{{UUID: uuid.NewString(), Provider: model.UploadProviderLocal, StorageKey: filename, FileName: filepath.Base(filename), FileSize: before.Size(), ReadType: model.ReadTypeVCF, Status: model.FileStatusCompleted, Source: model.DataAssetSourceLocalImport, ValidationStatus: "valid", ValidationSHA256: hex.EncodeToString(digest.Sum(nil))}}, nil
}
