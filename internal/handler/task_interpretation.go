package handler

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

func isTaskEditRequest(method, route string) bool {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return false
	}
	// Viewing, downloading and saving personal filters do not alter the interpretation.
	for _, suffix := range []string{"/igv/urls", "/igv/snapshot", "/query", "/export", "/views", "/result-package/prepare", "/downloads/prepare", "/downloads/quote", "/downloads/issue", "/assessment/context"} {
		if strings.HasSuffix(route, suffix) {
			return false
		}
	}
	return true
}

// Serialize task mutations across server replicas. A dedicated advisory-lock
// connection avoids deadlocks with existing services' own transactions.
func (h *TaskHandler) InterpretationEditGuard(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		taskID = c.Param("uuid")
	}
	if taskID == "" || !isTaskEditRequest(c.Request.Method, c.FullPath()) {
		return
	}
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		c.Abort()
		return
	}
	sqlDB, err := database.GetDB().DB()
	if err != nil {
		ErrorInternal(c, "无法核验任务编辑状态")
		c.Abort()
		return
	}
	conn, err := sqlDB.Conn(c.Request.Context())
	if err != nil {
		ErrorInternal(c, "无法核验任务编辑状态")
		c.Abort()
		return
	}
	defer conn.Close()
	key := "task-interpretation:" + task.UUID
	if _, err = conn.ExecContext(c.Request.Context(), "SELECT pg_advisory_lock(hashtextextended($1, 0))", key); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		ErrorInternal(c, "无法核验任务编辑状态")
		c.Abort()
		return
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
		defer cancel()
		var unlocked bool
		if err := conn.QueryRowContext(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", key).Scan(&unlocked); err != nil || !unlocked {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	task, ok = requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		c.Abort()
		return
	}
	if task.InterpretationCompletedAt != nil && !strings.HasSuffix(c.FullPath(), "/interpretation-completion") {
		ErrorConflict(c, service.ErrInterpretationCompleted.Error())
		c.Abort()
		return
	}
	c.Next()
}

func (h *TaskHandler) SetInterpretationCompleted(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	var request struct {
		Completed *bool  `json:"completed" binding:"required"`
		AttemptID string `json:"attemptId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorBadRequest(c, "需要明确的完成状态和执行批次")
		return
	}
	changed, err := h.svc.SetInterpretationCompleted(c.Request.Context(), task, *request.Completed, request.AttemptID, taskActorFromContext(c).Email)
	if errors.Is(err, service.ErrInterpretationAttemptChanged) {
		ErrorConflict(c, "任务执行批次已变更，请刷新后重试")
		return
	}
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, changed.ToDetailResponse())
}
