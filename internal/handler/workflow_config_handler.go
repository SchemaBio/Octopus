package handler

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

type WorkflowConfigHandler struct {
	svc *service.WorkflowConfigService
}

func NewWorkflowConfigHandler() *WorkflowConfigHandler {
	return &WorkflowConfigHandler{svc: service.NewWorkflowConfigService()}
}

func (h *WorkflowConfigHandler) List(c *gin.Context) {
	items, err := h.svc.List()
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	Success(c, items)
}

func (h *WorkflowConfigHandler) Update(c *gin.Context) {
	var req model.WorkflowThresholdUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	userID, _ := c.Get("user_id")
	id, _ := userID.(uint)
	item, err := h.svc.Update(c.Param("template"), c.Param("genome"), req, id)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, item)
}
