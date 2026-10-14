package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCNVBaselineNotFound = errors.New("CNV 基线不存在")
var ErrCNVBaselineInUse = errors.New("CNV 基线正在运行或被使用，无法删除")

func (s *CNVBaselineService) Delete(ctx context.Context, id string, actor model.OverlayActor) error {
	return deleteCNVBaseline(database.GetDB().WithContext(ctx), strings.TrimSpace(id), actor)
}

// Remove only the registry and pair selections. Task history, charges and
// archived objects remain subject to their existing retention policies.
func deleteCNVBaseline(db *gorm.DB, id string, actor model.OverlayActor) error {
	return db.Transaction(func(tx *gorm.DB) error {
		scope := func() *gorm.DB {
			query := tx.Where("uuid = ?", id)
			if actor.Role != string(model.SystemRoleSuperAdmin) {
				if actor.OrgID != "" {
					query = query.Where("external_org_id = ?", actor.OrgID)
				} else {
					query = query.Where("external_org_id = '' AND created_by = ?", actor.UserID)
				}
			}
			return query
		}
		var baseline model.CNVBaseline
		if err := scope().First(&baseline).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCNVBaselineNotFound
			}
			return err
		}
		// Archive registration locks the task first, then the baseline. Keep the
		// same order and serialize against retries and status changes.
		var task model.Task
		if baseline.TaskUUID != "" {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid = ?", baseline.TaskUUID).First(&task).Error; err != nil {
				if baseline.Status != model.TaskStatusCompleted || baseline.OutputPath == "" {
					return err
				}
				task.Status = model.TaskStatusCompleted
			}
		} else if baseline.Status == model.TaskStatusCompleted && baseline.OutputPath != "" {
			task.Status = model.TaskStatusCompleted
		}

		switch task.Status {
		case model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusCancelled:
		default:
			return fmt.Errorf("%w：请先在任务中心结束校正任务", ErrCNVBaselineInUse)
		}
		if err := scope().Clauses(clause.Locking{Strength: "UPDATE"}).First(&baseline).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCNVBaselineNotFound
			}
			return err
		}
		var count int64
		if err := tx.Model(&model.Pipeline{}).Where("cnv_baseline_id = ?", baseline.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("%w：请先解除流程中的基线选择", ErrCNVBaselineInUse)
		}
		// Parse saved snapshots without depending on PostgreSQL JSON casts;
		// historical terminal tasks keep their snapshots after deletion.
		var tasks []model.Task
		if err := tx.Select("uuid", "pipeline_snapshot_json").Where("status NOT IN ?", []model.TaskStatus{model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusCancelled, model.TaskStatusPendingInterpretation}).Find(&tasks).Error; err != nil {
			return err
		}
		for _, item := range tasks {
			if item.PipelineSnapshotJSON == "" || item.PipelineSnapshotJSON == "{}" {
				continue
			}
			var snap pipelineResourceSnapshot
			if err := decodePipelineSnapshot(item.PipelineSnapshotJSON, &snap); err != nil {
				return err
			}
			if snap.BaselineID == baseline.ID || snap.BaselineUUID == baseline.UUID {
				return fmt.Errorf("%w：仍有未结束任务使用此基线", ErrCNVBaselineInUse)
			}
		}
		if err := tx.Where("baseline_id = ?", baseline.ID).Delete(&model.CNVBaselineReadPair{}).Error; err != nil {
			return err
		}
		return tx.Delete(&baseline).Error
	})
}
