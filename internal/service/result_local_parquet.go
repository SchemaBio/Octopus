package service

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *ResultService) localBrowserStorage() bool {
	return s.cfg != nil && !s.cfg.IsSaaS() && s.cfg.Storage.Provider == "local"
}

// Only manifest-declared files inside this task's archive may become datasets.
func (s *ResultService) localResultSources(task *model.Task) (string, []byte, map[string]string, error) {
	dir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
		return "", nil, nil, err
	}
	raw, err := (&Archiver{cfg: s.cfg}).readArchiveJSONFile(dir, "outputs.resolved.json")
	if err != nil {
		return "", nil, nil, err
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return "", nil, nil, err
	}
	sources := map[string]string{}
	// Prefer Parquet; header-only reports can be archived as text by the Agent.
	refs := append(manifestParquetRefs(manifest), manifestTextRefs(manifest)...)
	for _, ref := range refs {
		candidate := ref
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(dir, candidate)
		}
		file, err := resolveArchiveRegularFile(dir, candidate)
		if err != nil {
			return "", nil, nil, err
		}
		for _, table := range []string{"snv-indel", "cnv-segment", "cnv-exon", "str", "mei", "mt", "upd", "roh"} {
			if !parquetTableMatch(table, filepath.Base(file)) {
				continue
			}
			if previous := sources[table]; previous != "" {
				if filepath.Ext(previous) == ".parquet" && filepath.Ext(file) != ".parquet" {
					continue
				}
				if previous != file {
					return "", nil, nil, fmt.Errorf("ambiguous local result table %s", table)
				}
			}
			sources[table] = file
		}
	}
	return dir, raw, sources, nil
}

func (s *ResultService) localParquetArchiveState(task *model.Task) model.ParquetResultState {
	state := model.ParquetResultState{Tables: []string{}, FieldProfileVersion: "parquet-fields-v2"}
	_, raw, sources, err := s.localResultSources(task)
	if err != nil {
		state.Reason = "本地结果归档不可用"
		return state
	}
	for table := range sources {
		state.Tables = append(state.Tables, table)
	}
	sort.Strings(state.Tables)
	state.Available = len(state.Tables) > 0
	state.ManifestVersion = fmt.Sprintf("%x", sha256.Sum256(raw))
	return state
}

func (s *ResultService) ensureLocalParquetDataset(ctx context.Context, task *model.Task, tenant, attempt, table string) (*model.ResultDataset, error) {
	_, manifest, sources, err := s.localResultSources(task)
	if err != nil {
		return nil, err
	}
	source := sources[table]
	if source == "" {
		return nil, fmt.Errorf("local archive has no declared result table %s", table)
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	identity := sha256.Sum256([]byte(tenant + "/" + task.UUID + "/" + attempt + "/" + table))
	id := hex.EncodeToString(identity[:])
	version := fmt.Sprintf("%x", sha256.Sum256(append(manifest, []byte(fmt.Sprintf("|%s|%d|%d", source, info.Size(), info.ModTime().UnixNano()))...)))
	var current model.ResultDataset
	err = database.DB.WithContext(ctx).Where("id=?", id).First(&current).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	cacheDir := s.cfg.ResultQuery.CacheDir
	if err == nil && current.ManifestVersion == version && current.ObjectSHA256 != "" {
		if st, e := os.Stat(filepath.Join(cacheDir, currentCacheName(id, current.ObjectSHA256))); e == nil && st.Size() == current.SourceSize {
			return &current, nil
		}
	}
	if err = os.MkdirAll(cacheDir, 0750); err != nil {
		return nil, err
	}
	input := source
	var expected *int64
	if filepath.Ext(source) != ".parquet" {
		converted, count, _, e := convertArchivedTextParquet(source, task.UUID)
		if e != nil {
			return nil, e
		}
		defer os.Remove(converted)
		input = converted
		expected = &count
	} else {
		for _, ext := range []string{".txt", ".tsv", ".csv"} {
			text := strings.TrimSuffix(source, ".parquet") + ext
			if _, e := os.Lstat(text); os.IsNotExist(e) {
				continue
			}
			if expected != nil {
				return nil, fmt.Errorf("ambiguous original local report")
			}
			safe, e := resolveArchiveRegularFile(filepath.Dir(source), text)
			if e != nil {
				return nil, e
			}
			count, e := localOriginalRows(safe)
			if e != nil {
				return nil, e
			}
			expected = &count
		}
	}
	file, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	tmp, err := os.CreateTemp(cacheDir, "local-parquet-*.tmp")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(file, maxCachedParquetBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written <= 0 || written > maxCachedParquetBytes {
		return nil, fmt.Errorf("invalid local Parquet size")
	}
	after, e := os.Stat(source)
	if e != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, fmt.Errorf("local result changed during preparation")
	}
	objectHash := hex.EncodeToString(hash.Sum(nil))
	cachePath := filepath.Join(cacheDir, currentCacheName(id, objectHash))
	if err = os.Rename(tmp.Name(), cachePath); err != nil {
		return nil, err
	}
	item := model.ResultDataset{ID: id, TenantID: tenant, TaskUUID: task.UUID, ExecutionAttemptID: attempt, Table: table,
		ObjectKey: filepath.Base(source), ObjectSHA256: objectHash, ManifestVersion: version, SourceSize: written,
		SourceLastModified: info.ModTime(), FieldsJSON: "[]", ExpectedRows: expected, DataVersion: objectHash, CreatedAt: time.Now().UTC()}
	err = database.DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "task_uuid"}, {Name: "execution_attempt_id"}, {Name: "table"}},
		DoUpdates: clause.AssignmentColumns([]string{"object_key", "object_sha256", "manifest_version", "source_size", "source_last_modified", "fields_json", "expected_rows", "rows", "data_version", "automatic_assessment_profile", "automatic_assessment_ready", "created_at"})}).Create(&item).Error
	return &item, err
}

// The handler authenticates task access before calling this scoped file lookup.
func (s *ResultService) LocalBrowserFile(ctx context.Context, task *model.Task, table, attempt, version string) (string, error) {
	if !s.localBrowserStorage() || !validParquetTable(table) || executionAttempt(task) != attempt || version == "" {
		return "", ErrAdjustmentConflict
	}
	d, err := s.ensureParquetDataset(ctx, task, model.TenantIDForTask(task), attempt, table)
	if err != nil {
		return "", err
	}
	if d.DataVersion != version {
		return "", ErrAdjustmentConflict
	}
	file := filepath.Join(s.cfg.ResultQuery.CacheDir, currentCacheName(d.ID, d.ObjectSHA256))
	return resolveArchiveRegularFile(s.cfg.ResultQuery.CacheDir, file)
}

func localOriginalRows(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if st.Size() > 256<<20 {
		return 0, fmt.Errorf("original report exceeds local read limit")
	}
	r := csv.NewReader(io.LimitReader(f, 256<<20+1))
	r.Comma = '\t'
	if filepath.Ext(path) == ".csv" {
		r.Comma = ','
	}
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return 0, fmt.Errorf("invalid original report header")
	}
	var count int64
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil || len(row) != len(header) {
			return 0, fmt.Errorf("invalid original report row structure")
		}
		count++
	}
	return count, nil
}
