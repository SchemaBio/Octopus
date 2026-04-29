package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
	"gorm.io/gorm"
)

// PedigreeRepository provides pedigree-specific operations
type PedigreeRepository struct {
	*Repository[model.Pedigree]
}

// NewPedigreeRepository creates a new pedigree repository
func NewPedigreeRepository() *PedigreeRepository {
	return &PedigreeRepository{
		Repository: NewRepository[model.Pedigree](),
	}
}

// PaginateByQuery finds pedigrees with pagination and search
func (r *PedigreeRepository) PaginateByQuery(query *model.PedigreeListQuery) ([]model.Pedigree, int64, error) {
	db := r.db.Model(&model.Pedigree{})

	if query.Search != "" {
		search := "%" + query.Search + "%"
		db = db.Where("name LIKE ? OR disease LIKE ?", search, search)
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

	var pedigrees []model.Pedigree
	offset := (page - 1) * pageSize
	err := db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&pedigrees).Error
	return pedigrees, total, err
}

// FindByIDWithMembers finds a pedigree by ID with members preloaded
func (r *PedigreeRepository) FindByIDWithMembers(id string) (*model.Pedigree, []model.PedigreeMember, error) {
	var pedigree model.Pedigree
	if err := r.db.First(&pedigree, "id = ?", id).Error; err != nil {
		return nil, nil, err
	}

	var members []model.PedigreeMember
	if err := r.db.Where("pedigree_id = ?", id).Order("generation ASC, position ASC").Find(&members).Error; err != nil {
		return &pedigree, nil, err
	}

	return &pedigree, members, nil
}

// CountMembers counts members in a pedigree
func (r *PedigreeRepository) CountMembers(pedigreeID string) int {
	var count int64
	r.db.Model(&model.PedigreeMember{}).Where("pedigree_id = ?", pedigreeID).Count(&count)
	return int(count)
}

// PedigreeMemberRepository provides pedigree member-specific operations
type PedigreeMemberRepository struct {
	*Repository[model.PedigreeMember]
}

// NewPedigreeMemberRepository creates a new pedigree member repository
func NewPedigreeMemberRepository() *PedigreeMemberRepository {
	return &PedigreeMemberRepository{
		Repository: NewRepository[model.PedigreeMember](),
	}
}

// FindByPedigreeID finds all members of a pedigree
func (r *PedigreeMemberRepository) FindByPedigreeID(pedigreeID string) ([]model.PedigreeMember, error) {
	var members []model.PedigreeMember
	err := r.db.Where("pedigree_id = ?", pedigreeID).Order("generation ASC, position ASC").Find(&members).Error
	return members, err
}

// DeleteByPedigreeID deletes all members of a pedigree
func (r *PedigreeMemberRepository) DeleteByPedigreeID(pedigreeID string) error {
	return r.db.Where("pedigree_id = ?", pedigreeID).Delete(&model.PedigreeMember{}).Error
}

// UpdateProband clears proband relation for pedigree and sets new proband
func (r *PedigreeMemberRepository) UpdateProband(pedigreeID, memberID string, tx *gorm.DB) error {
	// Clear existing proband
	if err := tx.Model(&model.PedigreeMember{}).
		Where("pedigree_id = ? AND relation = ?", pedigreeID, model.RelationProband).
		Update("relation", model.RelationOther).Error; err != nil {
		return err
	}

	// Set new proband
	return tx.Model(&model.PedigreeMember{}).
		Where("id = ? AND pedigree_id = ?", memberID, pedigreeID).
		Update("relation", model.RelationProband).Error
}
