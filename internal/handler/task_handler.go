package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskHandler struct {
	cfg       *config.Config
	svc       *service.TaskService
	sampleSvc *service.SampleService
	taskRepo  *repository.TaskRepository
}

func NewTaskHandler(cfg *config.Config) *TaskHandler {
	return &TaskHandler{
		cfg:       cfg,
		svc:       service.NewTaskService(cfg),
		sampleSvc: service.NewSampleService(cfg),
		taskRepo:  repository.NewTaskRepository(),
	}
}

// CVMStateEvent receives lifecycle callbacks from the trusted Squid control
// plane. It intentionally does not use user JWT authentication.
func (h *TaskHandler) CVMStateEvent(c *gin.Context) {
	if h.cfg == nil || !h.cfg.ExternalAuth.Enabled || h.cfg.ExternalAuth.SharedSecret == "" {
		ErrorUnauthorized(c, "CVM callback authentication is disabled")
		return
	}
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	parts := strings.SplitN(authorization, " ", 2)
	token := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		token = strings.TrimSpace(parts[1])
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.cfg.ExternalAuth.SharedSecret)) != 1 {
		ErrorUnauthorized(c, "Invalid CVM callback credentials")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var event model.CVMStateEvent
	if err := c.ShouldBindJSON(&event); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if err := h.svc.HandleCVMStateEvent(event); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

func taskActorFromContext(c *gin.Context) model.OverlayActor {
	userID, email, role, _ := middleware.GetCurrentUser(c)
	orgID, _ := middleware.GetCurrentOrg(c)
	return model.OverlayActor{
		UserID: userID,
		Email:  email,
		Role:   role,
		OrgID:  orgID,
	}
}

// CreateTask creates a new task
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req model.TaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	_, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	task, err := h.svc.CreateTask(c.Request.Context(), &req, taskActorFromContext(c))
	if err != nil {
		ErrorTaskOperation(c, err)
		return
	}

	SuccessCreated(c, task.ToResponse())
}

// EstimateTask previews the server-authoritative runtime estimate used for
// Squid credit pre-deduction without creating a task.
func (h *TaskHandler) EstimateTask(c *gin.Context) {
	var req model.TaskEstimateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	estimate, err := h.svc.EstimateTask(&req, taskActorFromContext(c))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, estimate)
}

func (h *TaskHandler) PreviewTaskBatch(c *gin.Context) {
	if _, _, _, ok := middleware.GetCurrentUser(c); !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3<<20)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		ErrorBadRequest(c, "file field is required")
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > 2<<20 {
		ErrorBadRequest(c, "XLSX 文件大小必须在 2 MB 以内")
		return
	}
	if !service.IsTaskBatchWorkbook(fileHeader.Filename) {
		ErrorBadRequest(c, "仅支持 .xlsx 文件")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		ErrorBadRequest(c, "无法读取上传文件")
		return
	}
	defer file.Close()
	rows, err := service.ParseTaskBatchWorkbook(file)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, h.svc.PreviewTaskBatch(rows, taskActorFromContext(c)))
}

func (h *TaskHandler) CreateTaskBatch(c *gin.Context) {
	if _, _, _, ok := middleware.GetCurrentUser(c); !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	var req model.TaskBatchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, fmt.Sprintf("invalid batch request: %v", err))
		return
	}
	Success(c, h.svc.CreateTaskBatch(c.Request.Context(), &req, taskActorFromContext(c)))
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
	if !applyTaskListScope(c, &query) {
		return
	}

	resp, err := h.svc.ListTasks(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// ListTasksAudit returns the enriched cross-org audit view of tasks for
// monitoring consumers (e.g. Cuttlefish). Same scoping as ListTasks via
// applyTaskListScope; PaginateByQuery honors search/created_since/updated_since.
func (h *TaskHandler) ListTasksAudit(c *gin.Context) {
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
	if !applyTaskListScope(c, &query) {
		return
	}

	resp, err := h.svc.ListTasksAudit(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// GetTaskStats returns the cross-org monitoring overview (totals, running,
// failed-24h, status distribution, failed result-import-7d) for Cuttlefish.
// Scoping matches the dashboard: SUPER_ADMIN (now reachable for Squid platform
// admins via applyExternalAuth role mapping) sees all; org users see their org.
func (h *TaskHandler) GetTaskStats(c *gin.Context) {
	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	orgID, hasOrg := middleware.GetCurrentOrg(c)
	isSuperAdmin := role == string(model.SystemRoleSuperAdmin)

	// scope applied to the tasks table (mirrors dashboard_handler.go:76-84).
	taskScope := func(db *gorm.DB) *gorm.DB {
		return taskDashboardScope(db, userID, orgID, hasOrg, isSuperAdmin)
	}

	db := database.GetDB()

	// Status distribution (all time) + total in one grouped query.
	statuses := []model.TaskStatus{
		model.TaskStatusQueued, model.TaskStatusWaitingData, model.TaskStatusRunning,
		model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusCancelled,
		model.TaskStatusPendingInterpretation,
	}
	dist := make(map[string]int, len(statuses))
	for _, s := range statuses {
		dist[string(s)] = 0
	}
	var statusRows []struct {
		Status model.TaskStatus
		Count  int64
	}
	if err := taskScope(db.Model(&model.Task{})).
		Select("status, COUNT(*) AS count").
		Group("status").
		Scan(&statusRows).Error; err != nil {
		ErrorInternal(c, "Failed to load task statistics")
		return
	}
	totalTasks := 0
	for _, row := range statusRows {
		dist[string(row.Status)] = int(row.Count)
		totalTasks += int(row.Count)
	}
	running := dist[string(model.TaskStatusRunning)]

	since24h := time.Now().Add(-24 * time.Hour)
	var failed24h int64
	if err := taskScope(db.Model(&model.Task{})).
		Where("status = ? AND updated_at >= ?", model.TaskStatusFailed, since24h).
		Count(&failed24h).Error; err != nil {
		ErrorInternal(c, "Failed to load task statistics")
		return
	}

	// Result-import failures: cross-table, handled by the batch repo.
	since7d := time.Now().Add(-7 * 24 * time.Hour)
	batchRepo := repository.NewResultImportBatchRepository()
	riFailed, err := batchRepo.CountFailedSinceScoped(&since7d, taskScope)
	if err != nil {
		ErrorInternal(c, "Failed to load task statistics")
		return
	}

	Success(c, model.TaskStatsResponse{
		TotalTasks:               totalTasks,
		RunningTasks:             running,
		FailedLast24h:            int(failed24h),
		StatusDistribution:       dist,
		ResultImportFailedLast7d: int(riFailed),
		WindowStart:              since24h.Format(time.RFC3339),
	})
}

// GetTask returns a single task by UUID
func (h *TaskHandler) GetTask(c *gin.Context) {
	id := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

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
	task, ok := requireTaskAccess(c, h.taskRepo, id)
	if !ok {
		return
	}

	if task.SampleID == "" {
		ErrorNotFound(c, "No sample associated with this task")
		return
	}

	sample, err := h.sampleSvc.GetSampleScoped(c.Request.Context(), task.SampleID, taskActorFromContext(c))
	if err != nil {
		ErrorNotFound(c, "Sample not found")
		return
	}

	Success(c, h.sampleSvc.SampleToDetailResponse(sample))
}

// UpdateTask updates task information
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	id := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

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
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

	task, err := h.svc.StartTask(c.Request.Context(), id, taskActorFromContext(c))
	if err != nil {
		ErrorTaskOperation(c, err)
		return
	}

	Success(c, task.ToResponse())
}

// StopTask stops a running task
func (h *TaskHandler) StopTask(c *gin.Context) {
	id := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

	task, err := h.svc.StopTask(c.Request.Context(), id, taskActorFromContext(c))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, task.ToResponse())
}

// RetryTask retries a failed task
func (h *TaskHandler) RetryTask(c *gin.Context) {
	id := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

	task, err := h.svc.RetryTask(c.Request.Context(), id, taskActorFromContext(c))
	if err != nil {
		ErrorTaskOperation(c, err)
		return
	}

	Success(c, task.ToResponse())
}

// RetryResultImport retries structured result import for a completed task archive.
func (h *TaskHandler) RetryResultImport(c *gin.Context) {
	id := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

	progress, err := h.svc.RetryResultImport(c.Request.Context(), id)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, progress)
}

// DeleteTask soft-deletes a non-running task. Queued tasks are cancelled first.
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	id := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

	if err := h.svc.DeleteTask(c.Request.Context(), id, taskActorFromContext(c)); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}

// GetTaskLogs returns task execution logs
func (h *TaskHandler) GetTaskLogs(c *gin.Context) {
	id := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

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
	if _, ok := requireTaskAccess(c, h.taskRepo, id); !ok {
		return
	}

	progress, err := h.svc.GetTaskProgress(c.Request.Context(), id)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, progress)
}

// ErrorTaskOperation maps trusted overlay and task-input failures to the
// public API contract. In particular, clients can distinguish invalid inputs
// (422), insufficient credits (402), concurrency limits (409), unavailable
// CVM capacity (503), and an explicit cloud launch failure (502).
func ErrorTaskOperation(c *gin.Context, err error) {
	if err == nil {
		ErrorInternal(c, "task operation failed")
		return
	}
	status := taskOperationStatus(err)
	Error(c, status, taskOperationMessage(err))
}

func taskOperationStatus(err error) int {
	var denied *service.OverlayDeniedError
	if errors.As(err, &denied) {
		reason := strings.ToLower(denied.Reason)
		if strings.Contains(reason, "concurrent") || strings.Contains(reason, "limit") {
			return http.StatusConflict
		}
		if strings.Contains(reason, "credit") || strings.Contains(reason, "balance") {
			return http.StatusPaymentRequired
		}
		return http.StatusUnprocessableEntity
	}
	var overlayHTTP *service.OverlayHTTPError
	if errors.As(err, &overlayHTTP) {
		switch overlayHTTP.Status {
		case http.StatusPaymentRequired, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusBadGateway, http.StatusServiceUnavailable:
			return overlayHTTP.Status
		case http.StatusBadRequest:
			return http.StatusUnprocessableEntity
		default:
			if overlayHTTP.Status >= 500 {
				return http.StatusBadGateway
			}
			return overlayHTTP.Status
		}
	}
	if service.OverlayDispatchOutcomeUnknown(err) {
		return http.StatusBadGateway
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "cvm dispatch is disabled") || strings.Contains(lower, "cvm dispatch requires an enabled overlay") || strings.Contains(lower, "cvm is unavailable") {
		return http.StatusServiceUnavailable
	}
	if strings.Contains(lower, "cvm dispatch") || strings.Contains(lower, "overlay task admission failed") {
		return http.StatusBadGateway
	}
	if strings.Contains(lower, "failed to save") || strings.Contains(lower, "persist ") || strings.Contains(lower, "database") {
		return http.StatusInternalServerError
	}
	return http.StatusUnprocessableEntity
}

func taskOperationMessage(err error) string {
	var overlayHTTP *service.OverlayHTTPError
	if errors.As(err, &overlayHTTP) {
		body := strings.TrimSpace(overlayHTTP.Body)
		if body != "" {
			var payload struct {
				Error  string `json:"error"`
				Reason string `json:"reason"`
			}
			if json.Unmarshal([]byte(body), &payload) == nil {
				if strings.TrimSpace(payload.Error) != "" {
					return strings.TrimSpace(payload.Error)
				}
				if strings.TrimSpace(payload.Reason) != "" {
					return strings.TrimSpace(payload.Reason)
				}
			}
			return body
		}
	}
	return strings.TrimSpace(err.Error())
}
