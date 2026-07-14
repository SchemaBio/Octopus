package handler

import (
	"net/http"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
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

	if _, _, _, ok := middleware.GetCurrentUser(c); !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	sample, err := h.svc.CreateSample(c.Request.Context(), &req, taskActorFromContext(c))
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
	if !applyCreatedByListScope(c, &query.CreatedBy, &query.IncludeAll) {
		return
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
	if !requireOwnerAccess(c, sample.CreatedBy, "Sample") {
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

	existing, err := h.svc.GetSample(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, "Sample not found")
		return
	}
	if !requireOwnerAccess(c, existing.CreatedBy, "Sample") {
		return
	}

	sample, err := h.svc.UpdateSample(c.Request.Context(), id, &req, taskActorFromContext(c))
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, h.svc.SampleToResponse(sample))
}

// ClearMatchedPair removes sequencing-data matching from a sample.
func (h *SampleHandler) ClearMatchedPair(c *gin.Context) {
	id := c.Param("id")

	existing, err := h.svc.GetSample(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, "Sample not found")
		return
	}
	if !requireOwnerAccess(c, existing.CreatedBy, "Sample") {
		return
	}

	sample, err := h.svc.ClearMatchedPair(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, h.svc.SampleToDetailResponse(sample))
}

// MatchFromUploadJob binds a completed upload job's read1/read2 files to a sample.
func (h *SampleHandler) MatchFromUploadJob(c *gin.Context) {
	id := c.Param("id")

	var req model.SampleMatchUploadJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	existing, err := h.svc.GetSample(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, "Sample not found")
		return
	}
	if !requireOwnerAccess(c, existing.CreatedBy, "Sample") {
		return
	}

	sample, err := h.svc.MatchFromUploadJob(c.Request.Context(), id, req.UploadJobID, taskActorFromContext(c))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, h.svc.SampleToDetailResponse(sample))
}

// DeleteSample deletes a sample
func (h *SampleHandler) DeleteSample(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.svc.GetSample(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, "Sample not found")
		return
	}
	if !requireOwnerAccess(c, existing.CreatedBy, "Sample") {
		return
	}

	if err := h.svc.DeleteSample(c.Request.Context(), id); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}
