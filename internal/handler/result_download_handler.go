package handler

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/gin-gonic/gin"
	"mime"
	"net/http"
)

type ResultDownloadHandler struct {
	svc   *service.ResultDownloadService
	tasks *repository.TaskRepository
}

func NewResultDownloadHandler(cfg *config.Config) *ResultDownloadHandler {
	return &ResultDownloadHandler{svc: service.NewResultDownloadService(cfg), tasks: repository.NewTaskRepository()}
}

func (h *ResultDownloadHandler) Catalog(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.tasks, c.Param("id"))
	if !ok {
		return
	}
	result, err := h.svc.Catalog(c.Request.Context(), task, c.Request.Method == http.MethodPost)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	Success(c, result)
}

func (h *ResultDownloadHandler) Active(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.tasks, c.Param("id"))
	if !ok {
		return
	}
	rows, err := h.svc.Active(c.Request.Context(), task, taskActorFromContext(c), downloadClientIP(c))
	if err != nil {
		ErrorInternal(c, "无法读取已有下载申请")
		return
	}
	c.Header("Cache-Control", "no-store")
	Success(c, rows)
}
func (h *ResultDownloadHandler) Quote(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.tasks, c.Param("id"))
	if !ok {
		return
	}
	var req struct {
		Kind   string `json:"kind"`
		FileID string `json:"file_id"`
	}
	if c.ShouldBindJSON(&req) != nil {
		ErrorBadRequest(c, "下载申请格式错误")
		return
	}
	result, err := h.svc.Quote(c.Request.Context(), task, taskActorFromContext(c), downloadClientIP(c), req.Kind, req.FileID)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	Success(c, result)
}
func (h *ResultDownloadHandler) Issue(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.tasks, c.Param("id"))
	if !ok {
		return
	}
	var req struct {
		ID string `json:"quote_id"`
	}
	if c.ShouldBindJSON(&req) != nil || req.ID == "" {
		ErrorBadRequest(c, "下载报价编号缺失")
		return
	}
	result, err := h.svc.Issue(c.Request.Context(), task, taskActorFromContext(c), downloadClientIP(c), req.ID)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	Success(c, result)
}

func downloadClientIP(c *gin.Context) string {
	if value, exists := c.Get("download_client_ip"); exists {
		ip, _ := value.(string)
		return ip
	}
	return c.ClientIP()
}

func (h *ResultDownloadHandler) File(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.tasks, c.Param("id"))
	if !ok {
		return
	}
	output, filename, err := h.svc.OpenZIP(c.Request.Context(), task, taskActorFromContext(c), downloadClientIP(c), c.Param("grant"), c.GetHeader("Range"))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	defer output.Body.Close()
	status := http.StatusOK
	headers := map[string]string{"Content-Disposition": mime.FormatMediaType("attachment", map[string]string{"filename": filename}), "Cache-Control": "private, no-store", "X-Content-Type-Options": "nosniff", "Accept-Ranges": "bytes"}
	if output.ContentRange != nil {
		status = http.StatusPartialContent
		headers["Content-Range"] = aws.ToString(output.ContentRange)
	}
	c.DataFromReader(status, aws.ToInt64(output.ContentLength), "application/zip", output.Body, headers)
}
