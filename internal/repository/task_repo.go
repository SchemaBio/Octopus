package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
)

// TaskRepository provides task-specific operations
type TaskRepository struct {
	*Repository[model.Task]
}

// NewTaskRepository creates a new task repository
func NewTaskRepository() *TaskRepository {
	return &TaskRepository{
		Repository: NewRepository[model.Task](),
	}
}

// FindByUUID finds a task by UUID
func (r *TaskRepository) FindByUUID(uuid string) (*model.Task, error) {
	return r.FindOneByCondition(map[string]interface{}{"uuid": uuid})
}

// FindByStatus finds tasks by status
func (r *TaskRepository) FindByStatus(status model.TaskStatus) ([]model.Task, error) {
	return r.FindByCondition(map[string]interface{}{"status": status})
}

// FindByProjectID finds tasks by project ID
func (r *TaskRepository) FindByProjectID(projectID uint) ([]model.Task, error) {
	return r.FindByCondition(map[string]interface{}{"project_id": projectID})
}

// FindBySampleID finds tasks by sample ID
func (r *TaskRepository) FindBySampleID(sampleID uint) ([]model.Task, error) {
	return r.FindByCondition(map[string]interface{}{"sample_id": sampleID})
}

// FindByCreator finds tasks created by a user
func (r *TaskRepository) FindByCreator(userID uint) ([]model.Task, error) {
	return r.FindByCondition(map[string]interface{}{"created_by": userID})
}

// UpdateStatus updates task status
func (r *TaskRepository) UpdateStatus(id string, status model.TaskStatus) error {
	return r.db.Model(&model.Task{}).Where("id = ?", id).Update("status", status).Error
}

// CountByStatusAndProject counts tasks by status and project
func (r *TaskRepository) CountByStatusAndProject(projectID uint) (map[model.TaskStatus]int64, error) {
	counts := make(map[model.TaskStatus]int64)
	statuses := []model.TaskStatus{
		model.TaskStatusPending,
		model.TaskStatusRunning,
		model.TaskStatusCompleted,
		model.TaskStatusFailed,
		model.TaskStatusCancelled,
	}

	for _, status := range statuses {
		var count int64
		err := r.db.Model(&model.Task{}).
			Where("project_id = ? AND status = ?", projectID, status).
			Count(&count).Error
		if err != nil {
			return nil, err
		}
		counts[status] = count
	}

	return counts, nil
}

// PaginateByQuery paginates tasks with multiple filters
func (r *TaskRepository) PaginateByQuery(query *model.TaskListQuery) ([]model.Task, int64, error) {
	db := r.db.Model(&model.Task{})

	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.Executor != "" {
		db = db.Where("executor = ?", query.Executor)
	}
	if query.SampleID > 0 {
		db = db.Where("sample_id = ?", query.SampleID)
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

	var tasks []model.Task
	offset := (page - 1) * pageSize
	err = db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&tasks).Error
	return tasks, total, err
}