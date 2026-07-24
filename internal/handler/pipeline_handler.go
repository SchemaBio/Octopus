package handler

import (
	"net/http"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
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

	if _, _, _, ok := middleware.GetCurrentUser(c); !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	pipeline, err := h.svc.CreatePipeline(c.Request.Context(), &req, taskActorFromContext(c))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	SuccessCreated(c, pipeline)
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
	if !applyCreatedByListScope(c, &query.CreatedBy, &query.IncludeAll) {
		return
	}
	if orgID, ok := middleware.GetCurrentOrg(c); ok && orgID != "" && !query.IncludeAll {
		query.ExternalOrgID = orgID
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

	pipeline, err := h.svc.GetPipeline(c.Request.Context(), id, taskActorFromContext(c))
	if err != nil {
		ErrorNotFound(c, "Pipeline not found")
		return
	}
	Success(c, pipeline)
}

// UpdatePipeline updates pipeline information
func (h *PipelineHandler) UpdatePipeline(c *gin.Context) {
	id := c.Param("id")

	var req model.PipelineUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	pipeline, err := h.svc.UpdatePipeline(c.Request.Context(), id, &req, taskActorFromContext(c))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, pipeline)
}

// DeletePipeline deletes a pipeline
func (h *PipelineHandler) DeletePipeline(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.DeletePipeline(c.Request.Context(), id, taskActorFromContext(c)); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}
