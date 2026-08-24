package handler

import (
	"errors"
	"net/http"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

type ResultPackageHandler struct {
	svc      *service.ResultPackageService
	taskRepo *repository.TaskRepository
}

func NewResultPackageHandler(cfg *config.Config) *ResultPackageHandler {
	return &ResultPackageHandler{svc: service.NewResultPackageService(cfg), taskRepo: repository.NewTaskRepository()}
}

func (h *ResultPackageHandler) Prepare(c *gin.Context) {
	task, ok := h.authorize(c)
	if !ok {
		return
	}
	result, err := h.svc.Prepare(c.Request.Context(), task)
	if err != nil {
		writeResultPackageError(c, err)
		return
	}
	Success(c, result)
}

func (h *ResultPackageHandler) Status(c *gin.Context) {
	task, ok := h.authorize(c)
	if !ok {
		return
	}
	result, err := h.svc.Status(c.Request.Context(), task)
	if err != nil {
		writeResultPackageError(c, err)
		return
	}
	Success(c, result)
}

func (h *ResultPackageHandler) authorize(c *gin.Context) (*model.Task, bool) {
	task, err := h.taskRepo.FindByUUID(c.Param("id"))
	if err != nil {
		ErrorNotFound(c, "Task not found")
		return nil, false
	}
	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return nil, false
	}
	// Result packages contain the complete execution output. Keep this stricter
	// than general org task visibility: only the task owner or platform admin
	// may prepare/query the package and receive a signed URL.
	if role != string(model.SystemRoleSuperAdmin) && task.CreatedBy != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		c.Abort()
		return nil, false
	}
	return task, true
}

func writeResultPackageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrResultPackageUnsupported):
		Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrResultPackageNotReady):
		Error(c, http.StatusConflict, err.Error())
	default:
		Error(c, http.StatusBadRequest, err.Error())
	}
}
