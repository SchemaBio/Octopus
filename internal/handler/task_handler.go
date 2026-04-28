package handler

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type TaskHandler struct {
	svc *service.TaskService
}

func NewTaskHandler(cfg *config.Config) *TaskHandler {
	return &TaskHandler{
		svc: service.NewTaskService(cfg),
	}
}

// CreateTask godoc
// @Summary Create a new task
// @Description Submit a new miniwdl task
// @Tags tasks
// @Accept json
// @Produce json
// @Param request body model.TaskCreateRequest true "Task creation request"
// @Success 201 {object} model.TaskResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/tasks [post]
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req model.TaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.svc.CreateTask(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, h.svc.TaskToResponse(task))
}

// ListTasks godoc
// @Summary List tasks
// @Description Get a list of tasks with optional filtering
// @Tags tasks
// @Produce json
// @Param status query string false "Filter by status"
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(10)
// @Success 200 {object} model.TaskListResponse
// @Router /api/v1/tasks [get]
func (h *TaskHandler) ListTasks(c *gin.Context) {
	var query model.TaskListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set defaults
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 10
	}

	resp, err := h.svc.ListTasks(c.Request.Context(), &query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetTask godoc
// @Summary Get a task by ID
// @Description Get detailed information about a specific task
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} model.TaskResponse
// @Failure 404 {object} map[string]string
// @Router /api/v1/tasks/{id} [get]
func (h *TaskHandler) GetTask(c *gin.Context) {
	id := c.Param("id")

	task, err := h.svc.GetTask(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.svc.TaskToResponse(task))
}

// CancelTask godoc
// @Summary Cancel a task
// @Description Cancel a running or pending task
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} model.TaskResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/v1/tasks/{id} [delete]
func (h *TaskHandler) CancelTask(c *gin.Context) {
	id := c.Param("id")

	task, err := h.svc.GetTask(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.CancelTask(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.svc.TaskToResponse(task))
}

// GetTaskLogs godoc
// @Summary Get task logs
// @Description Get the execution logs of a task
// @Tags tasks
// @Produce plain
// @Param id path string true "Task ID"
// @Success 200 {string} string "Task logs"
// @Failure 404 {object} map[string]string
// @Router /api/v1/tasks/{id}/logs [get]
func (h *TaskHandler) GetTaskLogs(c *gin.Context) {
	id := c.Param("id")

	logs, err := h.svc.GetTaskLogs(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.String(http.StatusOK, logs)
}

// GetTaskProgress godoc
// @Summary Get task progress
// @Description Get task status with Sepiida real-time progress
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} model.TaskProgressResponse
// @Failure 404 {object} map[string]string
// @Router /api/v1/tasks/{id}/progress [get]
func (h *TaskHandler) GetTaskProgress(c *gin.Context) {
	id := c.Param("id")

	progress, err := h.svc.GetTaskProgress(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, progress)
}