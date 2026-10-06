package repository

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Insert once: never move the deadline when a completion is replayed.
func RegisterBAMRetention(tx *gorm.DB, task *model.Task, bucket string) error {
	if task == nil || task.Executor != model.ExecutorCVM || task.Status != model.TaskStatusCompleted || task.FinishedAt == nil || task.FinishedAt.IsZero() {
		return nil
	}
	for _, id := range []string{task.UUID, task.ExternalOrgID, task.ExecutionAttemptID} {
		if _, err := uuid.Parse(id); err != nil {
			return nil
		}
	}
	job := model.BAMRetentionJob{ID: uuid.NewString(), TaskUUID: task.UUID, OrgID: task.ExternalOrgID, AttemptID: task.ExecutionAttemptID, Bucket: bucket, CompletedAt: task.FinishedAt.UTC(), ExpiresAt: task.FinishedAt.UTC().Add(7 * 24 * time.Hour), Status: "pending", PlanJSON: "[]"}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_uuid"}, {Name: "org_id"}, {Name: "attempt_id"}}, DoNothing: true}).Create(&job).Error
}

func (r *TaskRepository) EnableBAMRetention(bucket string) { r.bamRetentionBucket = bucket }
