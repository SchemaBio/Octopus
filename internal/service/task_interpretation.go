package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInterpretationAttemptChanged = errors.New("任务执行批次已变更，请刷新后重试")

var ErrInterpretationCompleted = errors.New("解读已完成，请先取消解读完成后再编辑")

func (s *TaskService) SetInterpretationCompleted(ctx context.Context, authorized *model.Task, completed bool, attemptID, actor string) (*model.Task, error) {
	var task model.Task
	err := database.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if authorized == nil {
			return fmt.Errorf("task is required")
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid = ? AND tenant_id = ?", authorized.UUID, model.TenantIDForTask(authorized)).First(&task).Error; err != nil {
			return err
		}
		if executionAttempt(&task) != attemptID {
			return ErrInterpretationAttemptChanged
		}
		if completed && task.Status != model.TaskStatusCompleted && task.Status != model.TaskStatusPendingInterpretation {
			return fmt.Errorf("仅分析完成的任务可以完成解读")
		}
		if completed && task.ExecutionPhase != "" && task.ExecutionPhase != "idle" && task.ExecutionPhase != "terminal" {
			return fmt.Errorf("请等待本次执行结束后再完成解读")
		}
		if (task.InterpretationCompletedAt != nil) == completed {
			return nil
		}
		var at *time.Time
		if completed {
			now := time.Now().UTC()
			at = &now
		} else {
			actor = ""
		}
		if err := tx.Model(&task).Updates(map[string]interface{}{"interpretation_completed_at": at, "interpretation_completed_by": actor}).Error; err != nil {
			return err
		}
		task.InterpretationCompletedAt = at
		task.InterpretationCompletedBy = actor
		return nil
	})
	return &task, err
}
