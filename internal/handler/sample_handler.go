package handler

import (
	"net/http"

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

// CreateSample creates a new sample
func (h *SampleHandler) CreateSample(c *gin.Context) {
	var req model.SampleCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	sample, err := h.svc.CreateSample(c.Request.Context(), &req, userID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	if sample == nil {
		ErrorConflict(c, "internal_id already exists")
		return
	}

	SuccessCreated(c, h.svc.SampleToResponse(sample))
}

// ListSamples returns paginated sample list
func (h *SampleHandler) ListSamples(c *gin.Context) {
	var query model.SampleListQuery
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

	resp, err := h.svc.ListSamples(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// GetSample returns a single sample by UUID
func (h *SampleHandler) GetSample(c *gin.Context) {
	id := c.Param("id")

	sample, err := h.svc.GetSample(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, "Sample not found")
		return
	}

	Success(c, h.svc.SampleToDetailResponse(sample))
}

// UpdateSample updates sample information
func (h *SampleHandler) UpdateSample(c *gin.Context) {
	id := c.Param("id")

	var req model.SampleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	sample, err := h.svc.UpdateSample(c.Request.Context(), id, &req)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, h.svc.SampleToResponse(sample))
}

// DeleteSample deletes a sample
func (h *SampleHandler) DeleteSample(c *gin.Context) {
	id := c.Param("id")

	if err := h.svc.DeleteSample(c.Request.Context(), id); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}
