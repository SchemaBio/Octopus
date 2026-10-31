package handler

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

func OrganizationResetStep(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.IsSaaS() {
			ErrorNotFound(c, "not found")
			return
		}
		if _, ok := authenticateMachineCallback(c, cfg, 4096); !ok {
			return
		}
		var req struct {
			OrgID string `json:"org_id"`
			ID    string `json:"id"`
			Phase string `json:"phase"`
		}
		if c.ShouldBindJSON(&req) != nil {
			ErrorBadRequest(c, "invalid reset request")
			return
		}
		state, err := service.StepOrganizationReset(c.Request.Context(), cfg, req.OrgID, req.ID, req.Phase)
		if err != nil {
			ErrorConflict(c, "organization reset pending; active work or cleanup must be resolved")
			return
		}
		c.JSON(200, gin.H{"state": state})
	}
}
