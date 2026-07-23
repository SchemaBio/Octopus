package handler

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

type CNVBaselineHandler struct{ svc *service.CNVBaselineService }

func NewCNVBaselineHandler(cfg *config.Config) *CNVBaselineHandler {
	return &CNVBaselineHandler{svc: service.NewCNVBaselineService(cfg)}
}

func (h *CNVBaselineHandler) Create(c *gin.Context) {
	var req model.CNVBaselineCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	item, err := h.svc.Create(c.Request.Context(), &req, taskActorFromContext(c))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	SuccessCreated(c, item)
}

func (h *CNVBaselineHandler) List(c *gin.Context) {
	items, err := h.svc.List(taskActorFromContext(c))
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	Success(c, items)
}

func (h *CNVBaselineHandler) Get(c *gin.Context) {
	item, err := h.svc.Get(c.Param("uuid"), taskActorFromContext(c))
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}
	Success(c, item)
}
