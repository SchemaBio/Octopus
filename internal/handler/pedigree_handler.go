package handler

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type PedigreeHandler struct {
	svc *service.PedigreeService
}

func NewPedigreeHandler(cfg *config.Config) *PedigreeHandler {
	return &PedigreeHandler{
		svc: service.NewPedigreeService(cfg),
	}
}

// List returns paginated pedigree list
func (h *PedigreeHandler) List(c *gin.Context) {
	var query model.PedigreeListQuery
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

// Get returns a pedigree with members
func (h *PedigreeHandler) Get(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.svc.Get(id)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, resp)
}

// Create creates a new pedigree
func (h *PedigreeHandler) Create(c *gin.Context) {
	var req model.PedigreeCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	pedigree, err := h.svc.Create(&req, userID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessCreated(c, model.PedigreeResponse{
		ID:        pedigree.ID,
		Name:      pedigree.Name,
		Disease:   pedigree.Disease,
		CreatedAt: pedigree.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: pedigree.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// Update updates a pedigree
func (h *PedigreeHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req model.PedigreeUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	pedigree, err := h.svc.Update(id, &req)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, model.PedigreeResponse{
		ID:        pedigree.ID,
		Name:      pedigree.Name,
		Disease:   pedigree.Disease,
		CreatedAt: pedigree.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: pedigree.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// Delete deletes a pedigree
func (h *PedigreeHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	if err := h.svc.Delete(id); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}

// SetProband sets a member as the proband
func (h *PedigreeHandler) SetProband(c *gin.Context) {
	pedigreeID := c.Param("id")
	memberID := c.Param("memberId")

	resp, err := h.svc.SetProband(pedigreeID, memberID)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, resp)
}

// --- Member handlers ---

// ListMembers returns all members of a pedigree
func (h *PedigreeHandler) ListMembers(c *gin.Context) {
	pedigreeID := c.Param("id")

	members, err := h.svc.ListMembers(pedigreeID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, members)
}

// GetMember returns a single member
func (h *PedigreeHandler) GetMember(c *gin.Context) {
	pedigreeID := c.Param("id")
	memberID := c.Param("memberId")

	member, err := h.svc.GetMember(pedigreeID, memberID)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, member)
}

// CreateMember creates a new member
func (h *PedigreeHandler) CreateMember(c *gin.Context) {
	pedigreeID := c.Param("id")

	var req model.PedigreeMemberCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	member, err := h.svc.CreateMember(pedigreeID, &req)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	SuccessCreated(c, member)
}

// UpdateMember updates a member
func (h *PedigreeHandler) UpdateMember(c *gin.Context) {
	pedigreeID := c.Param("id")
	memberID := c.Param("memberId")

	var req model.PedigreeMemberUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	member, err := h.svc.UpdateMember(pedigreeID, memberID, &req)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, member)
}

// DeleteMember deletes a member
func (h *PedigreeHandler) DeleteMember(c *gin.Context) {
	pedigreeID := c.Param("id")
	memberID := c.Param("memberId")

	if err := h.svc.DeleteMember(pedigreeID, memberID); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}
