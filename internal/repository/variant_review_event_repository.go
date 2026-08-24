package repository

import (
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

// VariantReviewEventRepository reads the append-only review audit stream.
type VariantReviewEventRepository struct{ db *gorm.DB }

func NewVariantReviewEventRepository() *VariantReviewEventRepository {
	return &VariantReviewEventRepository{db: database.GetDB()}
}

func (r *VariantReviewEventRepository) List(q *model.VariantReviewEventListQuery) ([]model.VariantReviewEvent, int64, error) {
	db := r.db.Model(&model.VariantReviewEvent{}).Where("task_uuid = ?", q.TaskUUID)
	if !q.IncludeAll {
		db = db.Where("tenant_id = ?", q.TenantID)
	}
	if q.VariantType != "" {
		db = db.Where("variant_type = ?", q.VariantType)
	}
	if q.VariantFingerprint != "" {
		db = db.Where("variant_fingerprint = ?", q.VariantFingerprint)
	}
	if q.ExecutionAttemptID != "" {
		db = db.Where("execution_attempt_id = ?", q.ExecutionAttemptID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	var rows []model.VariantReviewEvent
	err := db.Order("recorded_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
