package repository

import "github.com/bioinfo/schema-platform/internal/model"

// ResultImportBatchRepository provides import batch audit operations.
type ResultImportBatchRepository struct {
	*Repository[model.ResultImportBatch]
}

// NewResultImportBatchRepository creates a result import batch repository.
func NewResultImportBatchRepository() *ResultImportBatchRepository {
	return &ResultImportBatchRepository{
		Repository: NewRepository[model.ResultImportBatch](),
	}
}

// FindLatestByTaskUUID returns recent import attempts for a task.
func (r *ResultImportBatchRepository) FindLatestByTaskUUID(taskUUID string, limit int) ([]model.ResultImportBatch, error) {
	if limit < 1 {
		limit = 20
	}
	var batches []model.ResultImportBatch
	err := r.db.Where("task_uuid = ?", taskUUID).
		Order("started_at DESC").
		Limit(limit).
		Find(&batches).Error
	return batches, err
}
