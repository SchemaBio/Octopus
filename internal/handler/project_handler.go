package handler

import (
	"net/http"
	"strconv"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type ProjectHandler struct {
	svc *service.ProjectService
}

func NewProjectHandler(cfg *config.Config) *ProjectHandler {
	return &ProjectHandler{
		svc: service.NewProjectService(cfg),
	}
}

// CreateProject creates a new project
func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var req model.ProjectCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	project, err := h.svc.CreateProject(c.Request.Context(), &req, userID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	if project == nil {
		ErrorConflict(c, "project_code already exists")
		return
	}

	SuccessCreated(c, h.svc.ProjectToResponse(project))
}

// ListProjects returns paginated project list
func (h *ProjectHandler) ListProjects(c *gin.Context) {
	var query model.ProjectListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 10
	}

	resp, err := h.svc.ListProjects(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// GetProject returns a single project by ID
func (h *ProjectHandler) GetProject(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ErrorBadRequest(c, "invalid project ID")
		return
	}

	project, err := h.svc.GetProject(c.Request.Context(), uint(id))
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, h.svc.ProjectToResponse(project))
}

// GetProjectSummary returns project summary
func (h *ProjectHandler) GetProjectSummary(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ErrorBadRequest(c, "invalid project ID")
		return
	}

	summary, err := h.svc.GetSummary(c.Request.Context(), uint(id))
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, summary)
}

// UpdateProject updates project information
func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ErrorBadRequest(c, "invalid project ID")
		return
	}

	var req model.ProjectUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	project, err := h.svc.UpdateProject(c.Request.Context(), uint(id), &req)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, h.svc.ProjectToResponse(project))
}

// DeleteProject deletes a project
func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ErrorBadRequest(c, "invalid project ID")
		return
	}

	if err := h.svc.DeleteProject(c.Request.Context(), uint(id)); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}
