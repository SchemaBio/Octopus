package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Stable ordinal membership lets saves validate identity without executing a
// paginated query. Prepared row counts are authoritative; available original
// report counts must also agree.
func (s *ResultService) verifyBrowserRow(ctx context.Context, task *model.Task, table, rowID string, ordinal *int64) (*model.ResultDataset, error) {
	if ordinal == nil {
		return s.verifyParquetRow(ctx, task, table, rowID)
	}
	d, err := s.ensureParquetDataset(ctx, task, model.TenantIDForTask(task), executionAttempt(task), table)
	if err != nil {
		return nil, err
	}
	if d.ExpectedRows != nil && d.Rows != *d.ExpectedRows {
		return nil, ErrParquetIncomplete
	}
	if !browserOrdinalMatches(d, rowID, *ordinal) {
		return nil, ErrAdjustmentConflict
	}
	return d, nil
}
func browserOrdinalMatches(d *model.ResultDataset, rowID string, ordinal int64) bool {
	if d == nil || ordinal < 0 || ordinal >= d.Rows {
		return false
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", d.ID, d.ObjectSHA256, ordinal)))
	return hex.EncodeToString(hash[:]) == rowID
}

// BrowserResultDataset authorizes only the current, validated immutable object.
// URLs are short-lived and must never be persisted in saved views or logs.
type BrowserResultDataset struct {
	Dataset      model.ResultDataset `json:"dataset"`
	URL          string              `json:"url"`
	AutomaticURL string              `json:"automaticUrl,omitempty"`
	ExpiresAt    time.Time           `json:"expiresAt"`
	Columns      []string            `json:"columns"`
	Aliases      map[string][]string `json:"aliases"`
}

func (s *ResultService) BrowserDataset(ctx context.Context, task *model.Task, table string) (*BrowserResultDataset, error) {
	if task == nil || !validParquetTable(table) {
		return nil, fmt.Errorf("invalid result table")
	}
	d, err := s.ensureParquetDataset(ctx, task, model.TenantIDForTask(task), executionAttempt(task), table)
	if err != nil {
		return nil, err
	}
	// First preparation validates source row count. Ordinary browser queries never
	// use the server query endpoint; the preparation service remains a one-off gate.
	if d.FieldsJSON == "[]" || d.FieldsJSON == "" || (d.Rows == 0 && (d.ExpectedRows == nil || *d.ExpectedRows != 0)) {
		if _, err = s.QueryParquetTable(ctx, task, table, model.ParquetQueryRequest{Limit: 1}); err != nil {
			return nil, err
		}
		if err = database.DB.WithContext(ctx).First(d, "id=?", d.ID).Error; err != nil {
			return nil, err
		}
	}
	if d.ExpectedRows != nil && d.Rows != *d.ExpectedRows {
		return nil, ErrParquetIncomplete
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	expires := time.Now().UTC().Add(10 * time.Minute)
	url, err := storage.presignRead(ctx, d.ObjectKey, 10*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("result read authorization failed")
	}
	result := &BrowserResultDataset{Dataset: *d, URL: url, ExpiresAt: expires, Columns: []string{}, Aliases: map[string][]string{}}
	if err := json.Unmarshal([]byte(d.FieldsJSON), &result.Columns); err != nil {
		return nil, fmt.Errorf("invalid result schema")
	}
	for _, col := range result.Columns {
		marker := "__field_marker__"
		normalized := normalizeParquetAPIItem(table, map[string]interface{}{col: marker})
		for key, val := range normalized {
			if text, ok := val.(string); ok && text == marker {
				result.Aliases[col] = append(result.Aliases[col], key)
			}
		}
	}
	if table == "snv-indel" {
		// Publish the versioned persisted baseline once, not once per page/filter.
		key := resultPackagePrefix(task) + "/browser-baselines/" + d.ID + "-" + d.DataVersion + "-" + automaticACMGProfile + ".jsonl"
		if _, err := storage.stat(ctx, key); err != nil {
			filename := filepath.Join(s.cfg.ResultQuery.AssessmentDir, d.ID+"-"+d.DataVersion+"-"+automaticACMGProfile+".jsonl")
			f, err := os.Open(filename)
			if err != nil {
				return nil, fmt.Errorf("automatic baseline unavailable")
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				return nil, err
			}
			if err = storage.putReader(ctx, key, "application/x-ndjson", f, info.Size()); err != nil {
				return nil, fmt.Errorf("automatic baseline publication failed")
			}
		}
		result.AutomaticURL, err = storage.presignRead(ctx, key, 10*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("automatic baseline authorization failed")
		}
	}
	return result, nil
}

type BrowserAdjustmentSnapshot struct {
	Revision uint64                      `json:"revision"`
	Items    []model.ResultRowAdjustment `json:"items"`
}

// Lock the dataset during snapshot reads. Writers use the same lock, so the
// cursor never moves past a committed change absent from this response.
func (s *ResultService) BrowserAdjustments(ctx context.Context, task *model.Task, table, attempt, version string, since *uint64) (*BrowserAdjustmentSnapshot, error) {
	if task == nil || !validParquetTable(table) || executionAttempt(task) != attempt {
		return nil, ErrAdjustmentConflict
	}
	result := &BrowserAdjustmentSnapshot{Items: []model.ResultRowAdjustment{}}
	err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.Task
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("uuid=?", task.UUID).First(&current).Error; err != nil {
			return err
		}
		if executionAttempt(&current) != attempt {
			return ErrAdjustmentConflict
		}
		var d model.ResultDataset
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=?", model.TenantIDForTask(task), task.UUID, attempt, table).First(&d).Error; err != nil {
			return err
		}
		if d.DataVersion != version || (since != nil && *since > d.AdjustmentRevision) {
			return ErrAdjustmentConflict
		}
		q := tx.Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND dataset_version=?", d.TenantID, task.UUID, attempt, table, version)
		if since != nil {
			q = q.Where("revision>?", *since)
		}
		if err := q.Order("revision ASC, row_id ASC").Find(&result.Items).Error; err != nil {
			return err
		}
		result.Revision = d.AdjustmentRevision
		return nil
	})
	return result, err
}
