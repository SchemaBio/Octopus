package repository

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
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

// FindAllOrdered finds all report templates, including inactive templates.
func (r *ReportTemplateRepository) FindAllOrdered() ([]model.ReportTemplate, error) {
	var templates []model.ReportTemplate
	err := r.db.Order("name ASC").Find(&templates).Error
	return templates, err
}

// FindByName finds a template by name
func (r *ReportTemplateRepository) FindByName(name string) (*model.ReportTemplate, error) {
	return r.FindOneByCondition(map[string]interface{}{"name": name, "is_active": true})
}

// FindAnyByName finds a template by name regardless of active state.
func (r *ReportTemplateRepository) FindAnyByName(name string) (*model.ReportTemplate, error) {
	var tmpl model.ReportTemplate
	err := r.db.Where("name = ?", name).First(&tmpl).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tmpl, nil
}

// FindAnyByID finds a template by ID regardless of active state.
func (r *ReportTemplateRepository) FindAnyByID(id string) (*model.ReportTemplate, error) {
	var tmpl model.ReportTemplate
	err := r.db.Where("id = ?", id).First(&tmpl).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tmpl, nil
}
