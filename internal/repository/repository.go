package repository

import (
	"github.com/bioinfo/schema-platform/internal/database"
	"gorm.io/gorm"
)

// Repository provides base CRUD operations
type Repository[T any] struct {
	db *gorm.DB
}

// NewRepository creates a new repository for the given type
func NewRepository[T any]() *Repository[T] {
	return &Repository[T]{
		db: database.GetDB(),
	}
}

// Create inserts a new record
func (r *Repository[T]) Create(entity *T) error {
	return r.db.Create(entity).Error
}

// Update updates an existing record
func (r *Repository[T]) Update(entity *T) error {
	return r.db.Save(entity).Error
}

// Delete deletes a record by ID
func (r *Repository[T]) Delete(id uint) error {
	var entity T
	return r.db.Delete(&entity, id).Error
}

// DeleteByID deletes a record by string ID
func (r *Repository[T]) DeleteByID(id string) error {
	var entity T
	return r.db.Delete(&entity, "id = ?", id).Error
}

// FindByID finds a record by ID
func (r *Repository[T]) FindByID(id uint) (*T, error) {
	var entity T
	err := r.db.First(&entity, id).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// FindByStringID finds a record by string ID
func (r *Repository[T]) FindByStringID(id string) (*T, error) {
	var entity T
	err := r.db.First(&entity, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// FindAll finds all records
func (r *Repository[T]) FindAll() ([]T, error) {
	var entities []T
	err := r.db.Find(&entities).Error
	return entities, err
}

// FindByCondition finds records by condition
func (r *Repository[T]) FindByCondition(condition interface{}) ([]T, error) {
	var entities []T
	err := r.db.Where(condition).Find(&entities).Error
	return entities, err
}

// FindOneByCondition finds one record by condition
func (r *Repository[T]) FindOneByCondition(condition interface{}) (*T, error) {
	var entity T
	err := r.db.Where(condition).First(&entity).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// Count counts all records
func (r *Repository[T]) Count() (int64, error) {
	var count int64
	var entity T
	err := r.db.Model(&entity).Count(&count).Error
	return count, err
}

// CountByCondition counts records by condition
func (r *Repository[T]) CountByCondition(condition interface{}) (int64, error) {
	var count int64
	var entity T
	err := r.db.Model(&entity).Where(condition).Count(&count).Error
	return count, err
}

// Paginate finds records with pagination
func (r *Repository[T]) Paginate(page, pageSize int) ([]T, int64, error) {
	var entities []T
	var total int64
	var entity T

	// Count total
	err := r.db.Model(&entity).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// Paginate
	offset := (page - 1) * pageSize
	err = r.db.Offset(offset).Limit(pageSize).Find(&entities).Error
	return entities, total, err
}

// PaginateByCondition finds records with pagination and condition
func (r *Repository[T]) PaginateByCondition(condition interface{}, page, pageSize int) ([]T, int64, error) {
	var entities []T
	var total int64
	var entity T

	// Count total
	err := r.db.Model(&entity).Where(condition).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// Paginate
	offset := (page - 1) * pageSize
	err = r.db.Where(condition).Offset(offset).Limit(pageSize).Find(&entities).Error
	return entities, total, err
}