package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
)

// ReportRepository provides report-specific operations
type ReportRepository struct {
	*Repository[model.Report]
}

func NewReportRepository() *ReportRepository {
	return &ReportRepository{
		Repository: NewRepository[model.Report](),
	}
}

// FindByTaskID finds all reports for a task
func (r *ReportRepository) FindByTaskID(taskID string) ([]model.Report, error) {
	var reports []model.Report
	err := r.db.Where("task_id = ?", taskID).Order("created_at DESC").Find(&reports).Error
	return reports, err
}

// ReportTemplateRepository provides report template operations
type ReportTemplateRepository struct {
	*Repository[model.ReportTemplate]
}

func NewReportTemplateRepository() *ReportTemplateRepository {
	return &ReportTemplateRepository{
		Repository: NewRepository[model.ReportTemplate](),
	}
}

// FindActive finds all active report templates
func (r *ReportTemplateRepository) FindActive() ([]model.ReportTemplate, error) {
	var templates []model.ReportTemplate
	err := r.db.Where("is_active = ?", true).Order("name ASC").Find(&templates).Error
	return templates, err
}

// FindByName finds a template by name
func (r *ReportTemplateRepository) FindByName(name string) (*model.ReportTemplate, error) {
	return r.FindOneByCondition(map[string]interface{}{"name": name, "is_active": true})
}
