package handler

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type GeneListHandler struct {
	svc *service.GeneListService
}

func NewGeneListHandler(cfg *config.Config) *GeneListHandler {
	return &GeneListHandler{
		svc: service.NewGeneListService(cfg),
	}
}

// List returns paginated gene list
func (h *GeneListHandler) List(c *gin.Context) {
	var query model.GeneListListQuery
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

	resp, err := h.svc.List(&query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// Get returns a single gene list
func (h *GeneListHandler) Get(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.svc.Get(id)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, resp)
}

// Create creates a new gene list
func (h *GeneListHandler) Create(c *gin.Context) {
	var req model.GeneListCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	resp, err := h.svc.Create(&req, userID)
	if err != nil {
		if err.Error() == "gene list name already exists" {
			ErrorConflict(c, err.Error())
		} else {
			ErrorInternal(c, err.Error())
		}
		return
	}

	SuccessCreated(c, resp)
}

// Update updates a gene list
func (h *GeneListHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req model.GeneListUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	resp, err := h.svc.Update(id, &req)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, resp)
}

// Delete deletes a gene list
func (h *GeneListHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	if err := h.svc.Delete(id); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}
