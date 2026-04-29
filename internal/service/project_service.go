package service

import (
	"context"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
)

// ProjectService handles project business logic
type ProjectService struct {
	cfg       *config.Config
	repo      *repository.ProjectRepository
	sampleRepo *repository.SampleRepository
	taskRepo   *repository.TaskRepository
}

// NewProjectService creates a new project service
func NewProjectService(cfg *config.Config) *ProjectService {
	return &ProjectService{
		cfg:        cfg,
		repo:       repository.NewProjectRepository(),
		sampleRepo: repository.NewSampleRepository(),
		taskRepo:   repository.NewTaskRepository(),
	}
}

// CreateProject creates a new project
func (s *ProjectService) CreateProject(ctx context.Context, req *model.ProjectCreateRequest, userID uint) (*model.Project, error) {
	// Check if project_code exists
	if s.repo.ExistsByProjectCode(req.ProjectCode) {
		return nil, nil // Already exists
	}

	project := &model.Project{
		ProjectCode: req.ProjectCode,
		ProjectName: req.ProjectName,
		Description: req.Description,
		Panel:       req.Panel,
		Status:      model.ProjectStatusActive,
		CreatedBy:   userID,
	}

	if err := s.repo.Create(project); err != nil {
		return nil, err
	}

	return project, nil
}

// GetProject gets a project by ID
func (s *ProjectService) GetProject(ctx context.Context, id uint) (*model.Project, error) {
	return s.repo.FindByID(id)
}

// GetProjectByProjectCode gets a project by project_code
func (s *ProjectService) GetProjectByProjectCode(ctx context.Context, projectCode string) (*model.Project, error) {
	return s.repo.FindByProjectCode(projectCode)
}

// ListProjects lists projects with pagination and filters
func (s *ProjectService) ListProjects(ctx context.Context, query *model.ProjectListQuery) (*model.ProjectListResponse, error) {
	projects, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.ProjectResponse, len(projects))
	for i, project := range projects {
		items[i] = s.ProjectToResponse(&project)
	}

	return &model.ProjectListResponse{
		Total: int(total),
		Items: items,
	}, nil
}

// UpdateProject updates a project
func (s *ProjectService) UpdateProject(ctx context.Context, id uint, req *model.ProjectUpdateRequest) (*model.Project, error) {
	project, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	if req.ProjectName != "" {
		project.ProjectName = req.ProjectName
	}
	if req.Description != "" {
		project.Description = req.Description
	}
	if req.Panel != "" {
		project.Panel = req.Panel
	}
	if req.Status != "" {
		project.Status = req.Status
	}

	if err := s.repo.Update(project); err != nil {
		return nil, err
	}

	return project, nil
}

// DeleteProject deletes a project
func (s *ProjectService) DeleteProject(ctx context.Context, id uint) error {
	// Check if project has samples
	samples, err := s.sampleRepo.FindByProjectID(id)
	if err != nil {
		return err
	}
	if len(samples) > 0 {
		// Unassign samples instead of deleting project
		sampleIDs := make([]uint, len(samples))
		for i, s := range samples {
			sampleIDs[i] = s.ID
		}
		if err := s.sampleRepo.AssignProject(sampleIDs, 0); err != nil {
			return err
		}
	}

	return s.repo.Delete(id)
}

// GetSummary gets project summary with sample and task counts
func (s *ProjectService) GetSummary(ctx context.Context, id uint) (*model.ProjectSummaryResponse, error) {
	return s.repo.GetSummary(id, s.sampleRepo, s.taskRepo)
}

// UpdateStatus updates project status
func (s *ProjectService) UpdateStatus(ctx context.Context, id uint, status model.ProjectStatus) error {
	return s.repo.UpdateStatus(id, status)
}

// GetProjectsByCreator gets projects created by a user
func (s *ProjectService) GetProjectsByCreator(ctx context.Context, userID uint) ([]model.Project, error) {
	return s.repo.FindByCreator(userID)
}

// ProjectToResponse converts project to response format
func (s *ProjectService) ProjectToResponse(project *model.Project) model.ProjectResponse {
	return model.ProjectResponse{
		ID:          project.ID,
		ProjectCode: project.ProjectCode,
		ProjectName: project.ProjectName,
		Description: project.Description,
		Panel:       project.Panel,
		Status:      project.Status,
		CreatedBy:   project.CreatedBy,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}
}