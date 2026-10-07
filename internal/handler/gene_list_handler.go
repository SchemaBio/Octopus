package handler

import (
	"errors"
	"net/http"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
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
	a := taskActorFromContext(c)
	query.CreatedBy = a.UserID
	query.OrgID = a.OrgID
	query.ActorRole = a.Role
	query.ActorOrgRole = a.OrgRole

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

	geneList, err := h.svc.GetScoped(id, taskActorFromContext(c))
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	resp, err := h.svc.Get(id)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	resp.CanMaintain = taskActorFromContext(c).ResourceMaintenance(geneList.CreatedBy)
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

	resp, err := h.svc.Create(&req, userID, taskActorFromContext(c))
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
	geneList, err := h.svc.GetScoped(id, taskActorFromContext(c))
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}
	if !taskActorFromContext(c).ResourceMaintenance(geneList.CreatedBy) {
		ErrorNotFound(c, "Gene list not found")
		return
	}

	resp, err := h.svc.Update(id, &req, taskActorFromContext(c))
	if err != nil {
		writeResourceError(c, err)
		return
	}
	Success(c, resp)
}

// Delete deletes a gene list
func (h *GeneListHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	geneList, err := h.svc.GetScoped(id, taskActorFromContext(c))
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}
	if !taskActorFromContext(c).ResourceMaintenance(geneList.CreatedBy) {
		ErrorNotFound(c, "Gene list not found")
		return
	}

	if err := h.svc.Delete(id, taskActorFromContext(c)); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *GeneListHandler) Publish(c *gin.Context) {
	var req struct {
		ExpectedRevision uint64 `json:"expected_revision"`
	}
	if c.ShouldBindJSON(&req) != nil {
		ErrorBadRequest(c, "expected_revision required")
		return
	}
	r, err := h.svc.Publish(c.Param("id"), taskActorFromContext(c), req.ExpectedRevision)
	if err != nil {
		writeResourceError(c, err)
		return
	}
	Success(c, r)
}
func writeResourceError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrResourceConflict) || err.Error() == "gene list name already exists" {
		ErrorConflict(c, err.Error())
	} else if errors.Is(err, service.ErrResourceForbidden) {
		ErrorNotFound(c, err.Error())
	} else {
		ErrorBadRequest(c, err.Error())
	}
}
