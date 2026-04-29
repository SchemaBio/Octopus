package handler

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type TaskHandler struct {
	svc       *service.TaskService
	sampleSvc *service.SampleService
}

func NewTaskHandler(cfg *config.Config) *TaskHandler {
	return &TaskHandler{
		svc:       service.NewTaskService(cfg),
		sampleSvc: service.NewSampleService(cfg),
	}
}

// CreateTask creates a new task
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req model.TaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	task, err := h.svc.CreateTask(c.Request.Context(), &req, userID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessCreated(c, task.ToResponse())
}

// ListTasks returns paginated task list
func (h *TaskHandler) ListTasks(c *gin.Context) {
	var query model.TaskListQuery
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

	resp, err := h.svc.ListTasks(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// GetTask returns a single task by UUID
func (h *TaskHandler) GetTask(c *gin.Context) {
	id := c.Param("id")

	task, err := h.svc.GetTask(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, task.ToDetailResponse())
}

// GetTaskSample returns the sample associated with a task
func (h *TaskHandler) GetTaskSample(c *gin.Context) {
	id := c.Param("id")

	task, err := h.svc.GetTask(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, "Task not found")
		return
	}

	if task.SampleID == "" {
		ErrorNotFound(c, "No sample associated with this task")
		return
	}

	sample, err := h.sampleSvc.GetSample(c.Request.Context(), task.SampleID)
	if err != nil {
		ErrorNotFound(c, "Sample not found")
		return
	}

	Success(c, h.sampleSvc.SampleToDetailResponse(sample))
}

// UpdateTask updates task information
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	id := c.Param("id")

	var req model.TaskUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	task, err := h.svc.UpdateTask(c.Request.Context(), id, &req)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, task.ToResponse())
}

// StartTask starts a queued or failed task
func (h *TaskHandler) StartTask(c *gin.Context) {
	id := c.Param("id")

	task, err := h.svc.StartTask(c.Request.Context(), id)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, task.ToResponse())
}

// StopTask stops a running task
func (h *TaskHandler) StopTask(c *gin.Context) {
	id := c.Param("id")

	task, err := h.svc.StopTask(c.Request.Context(), id)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, task.ToResponse())
}

// RetryTask retries a failed task
func (h *TaskHandler) RetryTask(c *gin.Context) {
	id := c.Param("id")

	task, err := h.svc.RetryTask(c.Request.Context(), id)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, task.ToResponse())
}

// CancelTask cancels a running or queued task
func (h *TaskHandler) CancelTask(c *gin.Context) {
	id := c.Param("id")

	if err := h.svc.CancelTask(c.Request.Context(), id); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}

// GetTaskLogs returns task execution logs
func (h *TaskHandler) GetTaskLogs(c *gin.Context) {
	id := c.Param("id")

	logs, err := h.svc.GetTaskLogs(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.String(http.StatusOK, logs)
}

// GetTaskProgress returns task progress with Sepiida integration
func (h *TaskHandler) GetTaskProgress(c *gin.Context) {
	id := c.Param("id")

	progress, err := h.svc.GetTaskProgress(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, progress)
}
