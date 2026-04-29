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

// FindBySampleID finds a sample by sample_id (business identifier)
func (r *SampleRepository) FindBySampleID(sampleID string) (*model.Sample, error) {
	return r.FindOneByCondition(map[string]interface{}{"sample_id": sampleID})
}

// ExistsBySampleID checks if sample_id exists
func (r *SampleRepository) ExistsBySampleID(sampleID string) bool {
	var count int64
	r.db.Model(&model.Sample{}).Where("sample_id = ?", sampleID).Count(&count)
	return count > 0
}

// FindByProjectID finds samples by project ID
func (r *SampleRepository) FindByProjectID(projectID uint) ([]model.Sample, error) {
	return r.FindByCondition(map[string]interface{}{"project_id": projectID})
}

// FindByStatus finds samples by status
func (r *SampleRepository) FindByStatus(status model.SampleStatus) ([]model.Sample, error) {
	return r.FindByCondition(map[string]interface{}{"status": status})
}

// FindByType finds samples by type
func (r *SampleRepository) FindByType(sampleType model.SampleType) ([]model.Sample, error) {
	return r.FindByCondition(map[string]interface{}{"sample_type": sampleType})
}

// UpdateStatus updates sample status
func (r *SampleRepository) UpdateStatus(id uint, status model.SampleStatus) error {
	return r.db.Model(&model.Sample{}).Where("id = ?", id).Update("status", status).Error
}

// CountByStatusAndProject counts samples by status and project
func (r *SampleRepository) CountByStatusAndProject(projectID uint) (map[model.SampleStatus]int64, error) {
	counts := make(map[model.SampleStatus]int64)
	statuses := []model.SampleStatus{
		model.SampleStatusPending,
		model.SampleStatusProcessing,
		model.SampleStatusCompleted,
		model.SampleStatusFailed,
	}

	for _, status := range statuses {
		var count int64
		query := r.db.Model(&model.Sample{}).Where("status = ?", status)
		if projectID > 0 {
			query = query.Where("project_id = ?", projectID)
		}
		err := query.Count(&count).Error
		if err != nil {
			return nil, err
		}
		counts[status] = count
	}

	return counts, nil
}

// PaginateByQuery paginates samples with multiple filters
func (r *SampleRepository) PaginateByQuery(query *model.SampleListQuery) ([]model.Sample, int64, error) {
	db := r.db.Model(&model.Sample{})

	if query.ProjectID > 0 {
		db = db.Where("project_id = ?", query.ProjectID)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.SampleType != "" {
		db = db.Where("sample_type = ?", query.SampleType)
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