package handler

import (
	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type OrgHandler struct {
	svc *service.OrganizationService
}

func NewOrgHandler(cfg *config.Config) *OrgHandler {
	return &OrgHandler{
		svc: service.NewOrganizationService(cfg),
	}
}

// ListOrganizations returns all organizations for the current user
func (h *OrgHandler) ListOrganizations(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	orgs, err := h.svc.GetUserOrganizations(userID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, model.OrgListResponse{Organizations: orgs})
}

// SwitchOrganization switches the current user's active organization
func (h *OrgHandler) SwitchOrganization(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	var req model.SwitchOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	org, err := h.svc.SwitchOrganization(userID, req.OrgID)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, org)
}
