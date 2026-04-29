package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
)

// PipelineRepository provides pipeline-specific operations
type PipelineRepository struct {
	*Repository[model.Pipeline]
}

// NewPipelineRepository creates a new pipeline repository
func NewPipelineRepository() *PipelineRepository {
	return &PipelineRepository{
		Repository: NewRepository[model.Pipeline](),
	}
}

// FindByUUID finds a pipeline by UUID
func (r *PipelineRepository) FindByUUID(uuid string) (*model.Pipeline, error) {
	return r.FindOneByCondition(map[string]interface{}{"id": uuid})
}

// FindByName finds a pipeline by name
func (r *PipelineRepository) FindByName(name string) (*model.Pipeline, error) {
	return r.FindOneByCondition(map[string]interface{}{"name": name})
}

// ExistsByName checks if a pipeline name exists
func (r *PipelineRepository) ExistsByName(name string) bool {
	var count int64
	r.db.Model(&model.Pipeline{}).Where("name = ?", name).Count(&count)
	return count > 0
}

// PaginateByQuery paginates pipelines with filters
func (r *PipelineRepository) PaginateByQuery(query *model.PipelineListQuery) ([]model.Pipeline, int64, error) {
	db := r.db.Model(&model.Pipeline{})

	if query.Search != "" {
		search := "%" + query.Search + "%"
		db = db.Where("name LIKE ? OR description LIKE ?", search, search)
	}

	var total int64
	err := db.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	var pipelines []model.Pipeline
	offset := (page - 1) * pageSize
	err = db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&pipelines).Error
	return pipelines, total, err
}
