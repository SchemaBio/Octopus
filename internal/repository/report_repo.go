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

// FindActiveByOwner finds active report templates owned by a user.
func (r *ReportTemplateRepository) FindActiveByOwner(ownerUserID uint) ([]model.ReportTemplate, error) {
	var templates []model.ReportTemplate
	err := r.db.Where("owner_user_id = ? AND is_active = ?", ownerUserID, true).Order("name ASC").Find(&templates).Error
	return templates, err
}

// FindAllByOwner finds all report templates owned by a user, including inactive templates.
func (r *ReportTemplateRepository) FindAllByOwner(ownerUserID uint) ([]model.ReportTemplate, error) {
	var templates []model.ReportTemplate
	err := r.db.Where("owner_user_id = ?", ownerUserID).Order("name ASC").Find(&templates).Error
	return templates, err
}

// FindActiveByIDAndOwner finds an active template by ID and owner.
func (r *ReportTemplateRepository) FindActiveByIDAndOwner(id string, ownerUserID uint) (*model.ReportTemplate, error) {
	return r.findOne("id = ? AND owner_user_id = ? AND is_active = ?", id, ownerUserID, true)
}

// FindActiveByNameAndOwner supports older clients while still enforcing ownership.
func (r *ReportTemplateRepository) FindActiveByNameAndOwner(name string, ownerUserID uint) (*model.ReportTemplate, error) {
	return r.findOne("name = ? AND owner_user_id = ? AND is_active = ?", name, ownerUserID, true)
}

// FindAnyByNameAndOwner finds a user's template by name regardless of active state.
func (r *ReportTemplateRepository) FindAnyByNameAndOwner(name string, ownerUserID uint) (*model.ReportTemplate, error) {
	return r.findOne("name = ? AND owner_user_id = ?", name, ownerUserID)
}

// FindAnyByIDAndOwner finds a user's template by ID regardless of active state.
func (r *ReportTemplateRepository) FindAnyByIDAndOwner(id string, ownerUserID uint) (*model.ReportTemplate, error) {
	return r.findOne("id = ? AND owner_user_id = ?", id, ownerUserID)
}

func (r *ReportTemplateRepository) findOne(query string, args ...interface{}) (*model.ReportTemplate, error) {
	var tmpl model.ReportTemplate
	err := r.db.Where(query, args...).First(&tmpl).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tmpl, nil
}
