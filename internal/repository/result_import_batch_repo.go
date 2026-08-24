package repository

import (
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

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

// CountFailedSinceScoped counts result_import_batches with status='failed'
// started since `since` (nil = all time). The compatibility scope callback is
// retained for dashboard callers that have not yet been migrated to a direct
// tenant predicate.
func (r *ResultImportBatchRepository) CountFailedSinceScoped(since *time.Time, scope func(*gorm.DB) *gorm.DB) (int64, error) {
	db := r.db.Model(&model.ResultImportBatch{}).
		Joins("JOIN tasks ON tasks.uuid = result_import_batches.task_uuid").
		Where("result_import_batches.status = ?", model.ResultImportBatchStatusFailed)
	if since != nil {
		db = db.Where("result_import_batches.started_at >= ?", *since)
	}
	if scope != nil {
		db = scope(db)
	}
	var count int64
	return count, db.Count(&count).Error
}

// ResultImportBatchAuditRow is the scanned row for the import-batch audit list.
type ResultImportBatchAuditRow struct {
	ID          uint
	TaskUUID    string
	Source      string
	Status      model.ResultImportBatchStatus
	Fingerprint string
	Error       string
	StartedAt   time.Time
	FinishedAt  *time.Time
	OrgID       string
}

// PaginateByQuery lists import batches with status/since filters. New callers
// use TenantID directly; old callers retain the task-ownership fallback.
func (r *ResultImportBatchRepository) PaginateByQuery(q *model.ResultImportBatchListQuery) ([]ResultImportBatchAuditRow, int64, error) {
	db := r.db.Model(&model.ResultImportBatch{})
	if q.TenantID != "" && !q.IncludeAll {
		db = db.Where("result_import_batches.tenant_id = ?", q.TenantID)
	} else {
		db = db.Joins("LEFT JOIN tasks ON tasks.uuid = result_import_batches.task_uuid")
		db = applyTaskActorScope(db, q.ExternalOrgID, q.UserID, q.IncludeAll)
	}
	if q.Status != "" {
		db = db.Where("result_import_batches.status = ?", q.Status)
	}
	if q.Since != nil {
		db = db.Where("result_import_batches.started_at >= ?", *q.Since)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	var rows []ResultImportBatchAuditRow
	offset := (page - 1) * pageSize
	selectColumns := `result_import_batches.id, result_import_batches.task_uuid, result_import_batches.source,
		result_import_batches.status, result_import_batches.fingerprint, result_import_batches.error,
		result_import_batches.started_at, result_import_batches.finished_at,
		COALESCE(tasks.external_org_id, '') AS org_id`
	if q.TenantID != "" && !q.IncludeAll {
		selectColumns = `result_import_batches.id, result_import_batches.task_uuid, result_import_batches.source,
			result_import_batches.status, result_import_batches.fingerprint, result_import_batches.error,
			result_import_batches.started_at, result_import_batches.finished_at, '' AS org_id`
	}
	err := db.Select(selectColumns).
		Order("result_import_batches.started_at DESC").Offset(offset).Limit(pageSize).Scan(&rows).Error
	return rows, total, err
}
