package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type HistoryBackfillSummary struct {
	Datasets        int                 `json:"datasets"`
	Candidates      int                 `json:"candidates"`
	Written         int                 `json:"written"`
	Skipped         int                 `json:"skipped"`
	Unresolved      []map[string]string `json:"unresolved"`
	Execute         bool                `json:"execute"`
	UnknownIdentity int                 `json:"unknown_identity"`
}

// Backfill does not start queues, migrations or billing. Existing indexed rows
// win over a migration and a concurrent adjustment wins via version checking.
func (s *ResultService) BackfillHistoryReports(ctx context.Context, execute bool, taskID string) (*HistoryBackfillSummary, error) {
	summary := &HistoryBackfillSummary{Execute: execute, Unresolved: []map[string]string{}}
	query := database.DB.WithContext(ctx).Model(&model.ResultDataset{})
	if taskID != "" {
		query = query.Where("task_uuid=?", taskID)
	}
	var datasets []model.ResultDataset
	if err := query.Find(&datasets).Error; err != nil {
		return nil, err
	}
	for _, d := range datasets {
		var task model.Task
		if err := database.DB.WithContext(ctx).Where("uuid=?", d.TaskUUID).First(&task).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		if model.TenantIDForTask(&task) != d.TenantID {
			summary.Unresolved = append(summary.Unresolved, map[string]string{"dataset": d.ID, "reason": "scope_mismatch"})
			continue
		}
		var adjustments []model.ResultRowAdjustment
		if err := database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND dataset_version=?", d.TenantID, d.TaskUUID, d.ExecutionAttemptID, d.Table, d.DataVersion).Find(&adjustments).Error; err != nil {
			return nil, err
		}
		targets := map[string]model.ResultRowAdjustment{}
		var previouslyReported []string
		if err := database.DB.WithContext(ctx).Model(&model.ResultRowAdjustmentEvent{}).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND dataset_version=? AND after_json->>'reported'='true'", d.TenantID, d.TaskUUID, d.ExecutionAttemptID, d.Table, d.DataVersion).Distinct("row_id").Pluck("row_id", &previouslyReported).Error; err != nil {
			return nil, err
		}
		reportedIDs := map[string]bool{}
		for _, id := range previouslyReported {
			reportedIDs[id] = true
		}
		var indexed []model.HistoryReport
		if err := database.DB.WithContext(ctx).Where("dataset_id=? AND dataset_version=?", d.ID, d.DataVersion).Find(&indexed).Error; err != nil {
			return nil, err
		}
		indexedByRow := map[string]model.HistoryReport{}
		for _, row := range indexed {
			indexedByRow[row.RowID] = row
		}
		for _, a := range adjustments {
			var payload map[string]interface{}
			if json.Unmarshal([]byte(a.PayloadJSON), &payload) != nil {
				summary.Unresolved = append(summary.Unresolved, map[string]string{"dataset": d.ID, "row": a.RowID, "reason": "invalid_adjustment_payload"})
				continue
			}
			if payload["reported"] != true && !reportedIDs[a.RowID] {
				continue
			}
			if existing, ok := indexedByRow[a.RowID]; ok && (existing.Deleted || existing.AdjustmentVersion >= a.Version) {
				summary.Skipped++
				continue
			}
			targets[a.RowID] = a
		}
		if len(targets) == 0 {
			continue
		}
		summary.Datasets++
		summary.Candidates += len(targets)
		if !execute {
			continue
		}
		cache, err := s.historyDatasetCache(ctx, &d)
		if err != nil {
			summary.Unresolved = append(summary.Unresolved, map[string]string{"dataset": d.ID, "reason": "source_unavailable"})
			continue
		}
		for offset := int64(0); offset < d.Rows && len(targets) > 0; offset += 1000 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			page, err := NewParquetReader().ReadPage(cache, offset, 1000)
			if err != nil {
				return nil, fmt.Errorf("history dataset %s could not be read", d.ID)
			}
			if page.TotalRows != d.Rows {
				return nil, ErrParquetIncomplete
			}
			for i, raw := range page.Rows {
				ordinal := offset + int64(i)
				h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", d.ID, d.ObjectSHA256, ordinal)))
				rowID := hex.EncodeToString(h[:])
				a, ok := targets[rowID]
				if !ok {
					continue
				}
				delete(targets, rowID)
				fields := compactHistoryFields(raw, d.Table)
				encoded, _ := json.Marshal(fields)
				source := model.HistoryReport{ID: historyReportID(&d, rowID), TenantID: d.TenantID, TaskUUID: d.TaskUUID, AttemptID: d.ExecutionAttemptID, Table: d.Table, DatasetID: d.ID, DatasetVersion: d.DataVersion, RowID: rowID, RowOrdinal: ordinal, FieldsJSON: string(encoded)}
				if existing, ok := indexedByRow[rowID]; ok {
					source = existing
				}
				var events []model.ResultRowAdjustmentEvent
				if err = database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND row_id=? AND dataset_version=?", d.TenantID, d.TaskUUID, d.ExecutionAttemptID, d.Table, rowID, d.DataVersion).Order("created_at ASC,id ASC").Find(&events).Error; err != nil {
					return nil, err
				}
				for _, event := range events {
					var before, after map[string]interface{}
					_ = json.Unmarshal([]byte(event.BeforeJSON), &before)
					_ = json.Unmarshal([]byte(event.AfterJSON), &after)
					if after["reported"] == true && before["reported"] != true {
						at := event.CreatedAt
						if !at.IsZero() {
							if source.FirstReportedAt == nil {
								source.FirstReportedAt = &at
							}
							source.LastReportedAt = &at
						}
						source.ReportedBy = event.Actor
						source.ReportedClassification = stringAdjustment(after, "acmgOverride")
						if source.ReportedClassification == "" {
							source.ReportedClassification = stringAdjustment(after, "acmgClassification")
						}
					}
				}
				if source.ReferenceIdentity == "" && executionAttempt(&task) == d.ExecutionAttemptID {
					source.Reference, source.ReferenceIdentity = historyReferenceIdentity(task.InputJSON)
				}
				source.GroupKey, source.IdentityKnown = historyGroupIdentity(d.Table, source.ReferenceIdentity, source.ID, fields)
				err = database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					var locked model.Task
					if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid=?", task.UUID).First(&locked).Error; err != nil {
						return err
					}
					var current model.ResultRowAdjustment
					if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND row_id=?", d.TenantID, d.TaskUUID, d.ExecutionAttemptID, d.Table, rowID).First(&current).Error; err != nil {
						return err
					}
					if current.Version != a.Version || current.DatasetVersion != d.DataVersion {
						return ErrAdjustmentConflict
					}
					var existing model.HistoryReport
					lookup := tx.Where("id=?", source.ID).First(&existing).Error
					if lookup == nil {
						if existing.Deleted || existing.AdjustmentVersion >= current.Version {
							return ErrAdjustmentConflict
						}
					} else if !errors.Is(lookup, gorm.ErrRecordNotFound) {
						return lookup
					}
					var payload map[string]interface{}
					if err := json.Unmarshal([]byte(current.PayloadJSON), &payload); err != nil {
						return err
					}
					source.Reported = payload["reported"] == true
					source.AdjustmentVersion = current.Version
					source.UpdatedAt = time.Now().UTC()
					var err error
					source.Classification, err = historyEffectiveClassification(tx, &d, rowID, payload)
					if err != nil {
						return err
					}
					source.Revision, err = model.NextHistoryRevision(tx, source.TenantID)
					if err != nil {
						return err
					}
					if lookup == nil {
						return tx.Save(&source).Error
					}
					return tx.Create(&source).Error
				})
				if errors.Is(err, ErrAdjustmentConflict) || errors.Is(err, gorm.ErrRecordNotFound) {
					summary.Skipped++
					continue
				}
				if err != nil {
					return nil, err
				}
				summary.Written++
				if !source.IdentityKnown {
					summary.UnknownIdentity++
				}
			}
		}
		for rowID := range targets {
			summary.Unresolved = append(summary.Unresolved, map[string]string{"dataset": d.ID, "row": rowID, "reason": "immutable_row_not_found"})
		}
	}
	return summary, nil
}
