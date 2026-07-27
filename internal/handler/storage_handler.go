package handler

import (
	"crypto/subtle"
	"net/http"
	"strings"

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
	if h.cfg == nil || !h.cfg.ExternalAuth.Enabled || h.cfg.ExternalAuth.SharedSecret == "" {
		ErrorUnauthorized(c, "Tenant storage authentication is disabled")
		return
	}
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	parts := strings.SplitN(authorization, " ", 2)
	token := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		token = strings.TrimSpace(parts[1])
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.cfg.ExternalAuth.SharedSecret)) != 1 {
		ErrorUnauthorized(c, "Invalid tenant storage credentials")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
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
