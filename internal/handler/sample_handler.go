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

type SampleHandler struct {
	svc *service.SampleService
}

func NewSampleHandler(cfg *config.Config) *SampleHandler {
	return &SampleHandler{
		svc: service.NewSampleService(cfg),
	}
}

// CreateSample godoc
// @Summary Create a new sample
// @Description Create a new biological sample
// @Tags samples
// @Accept json
// @Produce json
// @Param request body model.SampleCreateRequest true "Sample creation request"
// @Success 201 {object} model.SampleResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/samples [post]
func (h *SampleHandler) CreateSample(c *gin.Context) {
	var req model.SampleCreateRequest
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

	sample, err := h.svc.CreateSample(c.Request.Context(), &req, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if sample == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "sample_id already exists"})
		return
	}

	c.JSON(http.StatusCreated, h.svc.SampleToResponse(sample))
}

// ListSamples godoc
// @Summary List samples
// @Description Get a list of samples with optional filtering
// @Tags samples
// @Produce json
// @Param project_id query int false "Filter by project ID"
// @Param status query string false "Filter by status"
// @Param sample_type query string false "Filter by sample type"
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(10)
// @Success 200 {object} model.SampleListResponse
// @Router /api/v1/samples [get]
func (h *SampleHandler) ListSamples(c *gin.Context) {
	var query model.SampleListQuery
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

	resp, err := h.svc.ListSamples(c.Request.Context(), &query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetSample godoc
// @Summary Get a sample by ID
// @Description Get detailed information about a specific sample
// @Tags samples
// @Produce json
// @Param id path int true "Sample ID"
// @Success 200 {object} model.SampleResponse
// @Failure 404 {object} map[string]string
// @Router /api/v1/samples/{id} [get]
func (h *SampleHandler) GetSample(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sample ID"})
		return
	}

	sample, err := h.svc.GetSample(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.svc.SampleToResponse(sample))
}

// UpdateSample godoc
// @Summary Update a sample
// @Description Update sample information
// @Tags samples
// @Accept json
// @Produce json
// @Param id path int true "Sample ID"
// @Param request body model.SampleUpdateRequest true "Sample update request"
// @Success 200 {object} model.SampleResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/v1/samples/{id} [put]
func (h *SampleHandler) UpdateSample(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sample ID"})
		return
	}

	var req model.SampleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sample, err := h.svc.UpdateSample(c.Request.Context(), uint(id), &req)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.svc.SampleToResponse(sample))
}

// DeleteSample godoc
// @Summary Delete a sample
// @Description Delete a sample by ID
// @Tags samples
// @Param id path int true "Sample ID"
// @Success 204 "No Content"
// @Failure 404 {object} map[string]string
// @Router /api/v1/samples/{id} [delete]
func (h *SampleHandler) DeleteSample(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sample ID"})
		return
	}

	if err := h.svc.DeleteSample(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// AssignProject godoc
// @Summary Assign samples to a project
// @Description Assign multiple samples to a project
// @Tags samples
// @Accept json
// @Param request body map[string]interface{} true "sample_ids and project_id"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /api/v1/samples/assign [post]
func (h *SampleHandler) AssignProject(c *gin.Context) {
	var req struct {
		SampleIDs []uint `json:"sample_ids" binding:"required"`
		ProjectID uint   `json:"project_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.AssignProject(c.Request.Context(), req.SampleIDs, req.ProjectID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "samples assigned to project successfully"})
}