package handler

import (
	"errors"
	"net/http"
	"path/filepath"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

type DataAssetHandler struct{ svc *service.DataAssetService }

func NewDataAssetHandler(cfg *config.Config) *DataAssetHandler {
	return &DataAssetHandler{svc: service.NewDataAssetService(cfg)}
}

func (h *DataAssetHandler) Config(c *gin.Context) { Success(c, h.svc.Config()) }

func (h *DataAssetHandler) List(c *gin.Context) {
	var query model.DataAssetListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	if role == string(model.SystemRoleSuperAdmin) {
		query.IncludeAll = true
	} else if orgID, hasOrg := middleware.GetCurrentOrg(c); hasOrg {
		query.OrgID = orgID
	} else {
		query.CreatedBy = userID
	}
	items, total, err := h.svc.List(&query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, items, total, query.Page, query.PageSize)
}

func (h *DataAssetHandler) Get(c *gin.Context) {
	asset, err := h.svc.Get(c.Param("uuid"), taskActorFromContext(c))
	if err != nil {
		ErrorNotFound(c, "Data asset not found")
		return
	}
	Success(c, model.DataAssetToResponse(asset))
}

func (h *DataAssetHandler) Download(c *gin.Context) {
	target, filename, err := h.svc.Download(c.Request.Context(), c.Param("uuid"), taskActorFromContext(c))
	if err != nil {
		if errors.Is(err, service.ErrDataAssetDownloadDisabled) {
			Error(c, http.StatusForbidden, "Data downloads are disabled in SaaS mode")
			return
		}
		ErrorNotFound(c, "Data asset not found")
		return
	}
	if target.URL != "" {
		c.Redirect(http.StatusTemporaryRedirect, target.URL)
		return
	}
	c.FileAttachment(target.LocalPath, filepath.Base(filename))
}

func (h *DataAssetHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("uuid"), taskActorFromContext(c)); err != nil {
		ErrorNotFound(c, "Data asset not found")
		return
	}
	c.Status(http.StatusNoContent)
}
