package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
)

// GeneListRepository provides gene list-specific operations
type GeneListRepository struct {
	*Repository[model.GeneList]
}

// NewGeneListRepository creates a new gene list repository
func NewGeneListRepository() *GeneListRepository {
	return &GeneListRepository{
		Repository: NewRepository[model.GeneList](),
	}
}

// PaginateByQuery finds gene lists with pagination and search
func (r *GeneListRepository) PaginateByQuery(query *model.GeneListListQuery) ([]model.GeneList, int64, error) {
	db := r.db.Model(&model.GeneList{})

	if query.Search != "" {
		search := "%" + query.Search + "%"
		db = db.Where("name LIKE ? OR description LIKE ?", search, search)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
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

	var lists []model.GeneList
	offset := (page - 1) * pageSize
	err := db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&lists).Error
	return lists, total, err
}

// FindByName finds a gene list by name
func (r *GeneListRepository) FindByName(name string) (*model.GeneList, error) {
	return r.FindOneByCondition(map[string]interface{}{"name": name})
}

// ExistsByName checks if a gene list with the given name exists
func (r *GeneListRepository) ExistsByName(name string) bool {
	var count int64
	r.db.Model(&model.GeneList{}).Where("name = ?", name).Count(&count)
	return count > 0
}
