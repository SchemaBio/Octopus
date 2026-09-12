package handler

import (
	"net/http"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

type StorageHandler struct {
	cfg *config.Config
	svc *service.TenantStorageService
}

func NewStorageHandler(cfg *config.Config) *StorageHandler {
	return &StorageHandler{cfg: cfg, svc: service.NewTenantStorageService(cfg)}
}

func (h *StorageHandler) InitializeTenant(c *gin.Context) {
	if _, ok := authenticateMachineCallback(c, h.cfg, 16<<10); !ok {
		return
	}
	var request struct {
		OrgID string `json:"org_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	prefix, err := h.svc.Initialize(c.Request.Context(), request.OrgID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"storage_prefix": prefix, "layout_version": 1})
}
