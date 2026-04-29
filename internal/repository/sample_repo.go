package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
)

// SampleRepository provides sample-specific operations
type SampleRepository struct {
	*Repository[model.Sample]
}

// NewSampleRepository creates a new sample repository
func NewSampleRepository() *SampleRepository {
	return &SampleRepository{
		Repository: NewRepository[model.Sample](),
	}
}

// FindByUUID finds a sample by UUID
func (r *SampleRepository) FindByUUID(uuid string) (*model.Sample, error) {
	return r.FindOneByCondition(map[string]interface{}{"uuid": uuid})
}

// FindByInternalID finds a sample by internal_id
func (r *SampleRepository) FindByInternalID(internalID string) (*model.Sample, error) {
	return r.FindOneByCondition(map[string]interface{}{"internal_id": internalID})
}

// ExistsByInternalID checks if internal_id exists
func (r *SampleRepository) ExistsByInternalID(internalID string) bool {
	var count int64
	r.db.Model(&model.Sample{}).Where("internal_id = ?", internalID).Count(&count)
	return count > 0
}

// FindByProjectID finds samples by project ID
func (r *SampleRepository) FindByProjectID(projectID uint) ([]model.Sample, error) {
	return r.FindByCondition(map[string]interface{}{"project_id": projectID})
}

// UpdateStatus updates sample status
func (r *SampleRepository) UpdateStatus(id uint, status model.SampleStatus) error {
	return r.db.Model(&model.Sample{}).Where("id = ?", id).Update("status", status).Error
}

// PaginateByQuery paginates samples with filters
func (r *SampleRepository) PaginateByQuery(query *model.SampleListQuery) ([]model.Sample, int64, error) {
	db := r.db.Model(&model.Sample{})

	if query.Search != "" {
		search := "%" + query.Search + "%"
		db = db.Where("internal_id LIKE ? OR uuid LIKE ?", search, search)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.SampleType != "" {
		db = db.Where("sample_type = ?", query.SampleType)
	}
	if query.ProjectID > 0 {
		db = db.Where("project_id = ?", query.ProjectID)
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

	var samples []model.Sample
	offset := (page - 1) * pageSize
	err = db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&samples).Error
	return samples, total, err
}

// AssignProject assigns samples to a project
func (r *SampleRepository) AssignProject(sampleIDs []uint, projectID uint) error {
	return r.db.Model(&model.Sample{}).Where("id IN ?", sampleIDs).Update("project_id", projectID).Error
}
