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

// CreateProject godoc
// @Summary Create a new project
// @Description Create a new project/batch
// @Tags projects
// @Accept json
// @Produce json
// @Param request body model.ProjectCreateRequest true "Project creation request"
// @Success 201 {object} model.ProjectResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/projects [post]
func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var req model.ProjectCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current user ID
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	project, err := h.svc.CreateProject(c.Request.Context(), &req, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if project == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "project_code already exists"})
		return
	}

	c.JSON(http.StatusCreated, h.svc.ProjectToResponse(project))
}

// ListProjects godoc
// @Summary List projects
// @Description Get a list of projects with optional filtering
// @Tags projects
// @Produce json
// @Param status query string false "Filter by status"
// @Param panel query string false "Filter by panel"
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(10)
// @Success 200 {object} model.ProjectListResponse
// @Router /api/v1/projects [get]
func (h *ProjectHandler) ListProjects(c *gin.Context) {
	var query model.ProjectListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set defaults
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 10
	}

	resp, err := h.svc.ListProjects(c.Request.Context(), &query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetProject godoc
// @Summary Get a project by ID
// @Description Get detailed information about a specific project
// @Tags projects
// @Produce json
// @Param id path int true "Project ID"
// @Success 200 {object} model.ProjectResponse
// @Failure 404 {object} map[string]string
// @Router /api/v1/projects/{id} [get]
func (h *ProjectHandler) GetProject(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project ID"})
		return
	}

	project, err := h.svc.GetProject(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.svc.ProjectToResponse(project))
}

// GetProjectSummary godoc
// @Summary Get project summary
// @Description Get project summary with sample and task counts
// @Tags projects
// @Produce json
// @Param id path int true "Project ID"
// @Success 200 {object} model.ProjectSummaryResponse
// @Failure 404 {object} map[string]string
// @Router /api/v1/projects/{id}/summary [get]
func (h *ProjectHandler) GetProjectSummary(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project ID"})
		return
	}

	summary, err := h.svc.GetSummary(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, summary)
}

// UpdateProject godoc
// @Summary Update a project
// @Description Update project information
// @Tags projects
// @Accept json
// @Produce json
// @Param id path int true "Project ID"
// @Param request body model.ProjectUpdateRequest true "Project update request"
// @Success 200 {object} model.ProjectResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/v1/projects/{id} [put]
func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project ID"})
		return
	}

	var req model.ProjectUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	project, err := h.svc.UpdateProject(c.Request.Context(), uint(id), &req)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.svc.ProjectToResponse(project))
}

// DeleteProject godoc
// @Summary Delete a project
// @Description Delete a project by ID (unassigns samples first)
// @Tags projects
// @Param id path int true "Project ID"
// @Success 204 "No Content"
// @Failure 404 {object} map[string]string
// @Router /api/v1/projects/{id} [delete]
func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project ID"})
		return
	}

	if err := h.svc.DeleteProject(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}