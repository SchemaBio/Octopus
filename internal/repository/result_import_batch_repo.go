package repository

import (
	"time"

	"github.com/bioinfo/schema-platform/internal/model"
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
// started since `since` (nil = all time), org-scoped by joining to tasks
// (ResultImportBatch itself carries no org). The scope func is provided by
// the handler (e.g. built from taskDashboardScope) to keep the repository
// free of handler-package concerns.
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

// ResultImportBatchAuditRow is the scanned row for the import-batch audit list:
// the batch columns plus the owning task's org (joined).
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

// PaginateByQuery lists import batches with status/since filters and org/user
// scoping via a JOIN to tasks (org lives on tasks.external_org_id). Mirrors
// the scope switch in TaskRepository.PaginateByQuery.
func (r *ResultImportBatchRepository) PaginateByQuery(q *model.ResultImportBatchListQuery) ([]ResultImportBatchAuditRow, int64, error) {
	db := r.db.Model(&model.ResultImportBatch{}).
		Joins("LEFT JOIN tasks ON tasks.uuid = result_import_batches.task_uuid")

	if !q.IncludeAll {
		switch {
		case q.ExternalOrgID != "":
			db = db.Where("tasks.external_org_id = ?", q.ExternalOrgID)
		case q.UserID != 0:
			db = db.Where("tasks.created_by = ?", q.UserID)
		default:
			db = db.Where("1 = 0")
		}
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
	err := db.Select(`result_import_batches.id, result_import_batches.task_uuid, result_import_batches.source,
		result_import_batches.status, result_import_batches.fingerprint, result_import_batches.error,
		result_import_batches.started_at, result_import_batches.finished_at,
		tasks.external_org_id AS org_id`).
		Order("result_import_batches.started_at DESC").Offset(offset).Limit(pageSize).Scan(&rows).Error
	return rows, total, err
}
