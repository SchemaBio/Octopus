package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
)

// ProjectRepository provides project-specific operations
type ProjectRepository struct {
	*Repository[model.Project]
}

// NewProjectRepository creates a new project repository
func NewProjectRepository() *ProjectRepository {
	return &ProjectRepository{
		Repository: NewRepository[model.Project](),
	}
}

// FindByProjectCode finds a project by project_code
func (r *ProjectRepository) FindByProjectCode(projectCode string) (*model.Project, error) {
	return r.FindOneByCondition(map[string]interface{}{"project_code": projectCode})
}

// ExistsByProjectCode checks if project_code exists
func (r *ProjectRepository) ExistsByProjectCode(projectCode string) bool {
	var count int64
	r.db.Model(&model.Project{}).Where("project_code = ?", projectCode).Count(&count)
	return count > 0
}

// FindByStatus finds projects by status
func (r *ProjectRepository) FindByStatus(status model.ProjectStatus) ([]model.Project, error) {
	return r.FindByCondition(map[string]interface{}{"status": status})
}

// FindByCreator finds projects created by a user
func (r *ProjectRepository) FindByCreator(userID uint) ([]model.Project, error) {
	return r.FindByCondition(map[string]interface{}{"created_by": userID})
}

// FindByPanel finds projects by panel type
func (r *ProjectRepository) FindByPanel(panel string) ([]model.Project, error) {
	return r.FindByCondition(map[string]interface{}{"panel": panel})
}

// UpdateStatus updates project status
func (r *ProjectRepository) UpdateStatus(id uint, status model.ProjectStatus) error {
	return r.db.Model(&model.Project{}).Where("id = ?", id).Update("status", status).Error
}

// PaginateByQuery paginates projects with multiple filters
func (r *ProjectRepository) PaginateByQuery(query *model.ProjectListQuery) ([]model.Project, int64, error) {
	db := r.db.Model(&model.Project{})

	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.Panel != "" {
		db = db.Where("panel = ?", query.Panel)
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

	var projects []model.Project
	offset := (page - 1) * pageSize
	err = db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&projects).Error
	return projects, total, err
}

// GetSummary gets project summary with sample and task counts
func (r *ProjectRepository) GetSummary(projectID uint, sampleRepo *SampleRepository, taskRepo *TaskRepository) (*model.ProjectSummaryResponse, error) {
	// Get project
	project, err := r.FindByID(projectID)
	if err != nil {
		return nil, err
	}

	// Count samples by status
	sampleCounts, err := sampleRepo.CountByStatusAndProject(projectID)
	if err != nil {
		return nil, err
	}

	// Count tasks by status
	taskCounts, err := taskRepo.CountByStatusAndProject(projectID)
	if err != nil {
		return nil, err
	}

	// Build summary
	summary := &model.ProjectSummaryResponse{
		Project: model.ProjectResponse{
			ID:          project.ID,
			ProjectCode: project.ProjectCode,
			ProjectName: project.ProjectName,
			Description: project.Description,
			Panel:       project.Panel,
			Status:      project.Status,
			CreatedBy:   project.CreatedBy,
			CreatedAt:   project.CreatedAt,
			UpdatedAt:   project.UpdatedAt,
		},
		TotalSamples:     sampleCounts[model.SampleStatusPending] + sampleCounts[model.SampleStatusProcessing] + sampleCounts[model.SampleStatusCompleted] + sampleCounts[model.SampleStatusFailed],
		PendingSamples:   sampleCounts[model.SampleStatusPending],
		ProcessingSamples: sampleCounts[model.SampleStatusProcessing],
		CompletedSamples:  sampleCounts[model.SampleStatusCompleted],
		FailedSamples:     sampleCounts[model.SampleStatusFailed],
		TotalTasks:       taskCounts[model.TaskStatusQueued] + taskCounts[model.TaskStatusRunning] + taskCounts[model.TaskStatusCompleted] + taskCounts[model.TaskStatusFailed] + taskCounts[model.TaskStatusCancelled],
		PendingTasks:     taskCounts[model.TaskStatusQueued],
		RunningTasks:     taskCounts[model.TaskStatusRunning],
		CompletedTasks:   taskCounts[model.TaskStatusCompleted],
		FailedTasks:      taskCounts[model.TaskStatusFailed] + taskCounts[model.TaskStatusCancelled],
	}

	return summary, nil
}