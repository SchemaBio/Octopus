package handler

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type PipelineHandler struct {
	svc *service.PipelineService
}

func NewPipelineHandler(cfg *config.Config) *PipelineHandler {
	return &PipelineHandler{
		svc: service.NewPipelineService(cfg),
	}
}

// CreatePipeline creates a new pipeline
func (h *PipelineHandler) CreatePipeline(c *gin.Context) {
	var req model.PipelineCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	pipeline, err := h.svc.CreatePipeline(c.Request.Context(), &req, userID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	if pipeline == nil {
		ErrorConflict(c, "Pipeline name already exists")
		return
	}

	SuccessCreated(c, pipeline.ToResponse())
}

// ListPipelines returns paginated pipeline list
func (h *PipelineHandler) ListPipelines(c *gin.Context) {
	var query model.PipelineListQuery
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

	resp, err := h.svc.ListPipelines(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// GetPipeline returns a single pipeline by UUID
func (h *PipelineHandler) GetPipeline(c *gin.Context) {
	id := c.Param("id")

	pipeline, err := h.svc.GetPipeline(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, "Pipeline not found")
		return
	}

	Success(c, pipeline.ToResponse())
}

// UpdatePipeline updates pipeline information
func (h *PipelineHandler) UpdatePipeline(c *gin.Context) {
	id := c.Param("id")

	var req model.PipelineUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	pipeline, err := h.svc.UpdatePipeline(c.Request.Context(), id, &req)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, pipeline.ToResponse())
}

// DeletePipeline deletes a pipeline
func (h *PipelineHandler) DeletePipeline(c *gin.Context) {
	id := c.Param("id")

	if err := h.svc.DeletePipeline(c.Request.Context(), id); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}
