package handler

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type ReportHandler struct {
	svc      *service.ReportService
	taskRepo *repository.TaskRepository
}

func NewReportHandler(cfg *config.Config) *ReportHandler {
	return &ReportHandler{
		svc:      service.NewReportService(cfg),
		taskRepo: repository.NewTaskRepository(),
	}
}

// ListReports returns all legacy persisted reports for a task.
func (h *ReportHandler) ListReports(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	reports, err := h.svc.ListByTaskID(taskID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, reports)
}

// CreateReport triggers report generation and streams the generated file.
func (h *ReportHandler) CreateReport(c *gin.Context) {
	taskID := c.Param("id")
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}

	var req model.ReportCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	_, email, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	download, err := h.svc.GenerateReportDownload(c.Request.Context(), task, &req, email)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	defer download.Body.Close()

	c.Header("Content-Type", download.ContentType)
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": download.FileName}))
	c.Header("Cache-Control", "no-store")
	if download.ContentLength >= 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", download.ContentLength))
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, download.Body); err != nil {
		if errors.Is(err, service.ErrReportDownloadTooLarge) {
			c.Error(fmt.Errorf("report download aborted: %w", err))
			return
		}
		c.Error(err)
	}
}

// UploadReport is disabled in direct-download report mode.
func (h *ReportHandler) UploadReport(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	ErrorGone(c, "uploaded reports are disabled; generate reports through the configured report API")
}

// UpdateReportStatus is disabled in direct-download report mode.
func (h *ReportHandler) UpdateReportStatus(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	ErrorGone(c, "stored report workflow is disabled; generate reports through the configured report API")
}

// DeleteReport is disabled in direct-download report mode.
func (h *ReportHandler) DeleteReport(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	ErrorGone(c, "stored report records are disabled; generate reports through the configured report API")
}

// GetReportDownloadURL is disabled in direct-download report mode.
func (h *ReportHandler) GetReportDownloadURL(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	ErrorGone(c, "stored report downloads are disabled; generate reports through the configured report API")
}

// ListTemplates returns active report templates.
func (h *ReportHandler) ListTemplates(c *gin.Context) {
	_, _, role, ok := middleware.GetCurrentUser(c)
	if ok && role == string(model.SystemRoleSuperAdmin) && c.Query("include_inactive") == "true" {
		templates, err := h.svc.ListTemplatesAdmin()
		if err != nil {
			ErrorInternal(c, err.Error())
			return
		}
		Success(c, templates)
		return
	}

	templates, err := h.svc.ListActiveTemplates()
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, templates)
}

// CreateTemplate creates a new report template.
func (h *ReportHandler) CreateTemplate(c *gin.Context) {
	var req model.ReportTemplateCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	tmpl, err := h.svc.CreateTemplate(&req)
	if err != nil {
		writeReportTemplateError(c, err)
		return
	}

	SuccessCreated(c, tmpl)
}

// UpdateTemplate updates a report template.
func (h *ReportHandler) UpdateTemplate(c *gin.Context) {
	var req model.ReportTemplateUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	tmpl, err := h.svc.UpdateTemplate(c.Param("id"), &req)
	if err != nil {
		writeReportTemplateError(c, err)
		return
	}

	Success(c, tmpl)
}

// UpdateTemplateStatus toggles a report template's active state.
func (h *ReportHandler) UpdateTemplateStatus(c *gin.Context) {
	var req model.ReportTemplateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	tmpl, err := h.svc.SetTemplateActive(c.Param("id"), req.IsActive)
	if err != nil {
		writeReportTemplateError(c, err)
		return
	}

	Success(c, tmpl)
}

// DeleteTemplate deletes an inactive report template.
func (h *ReportHandler) DeleteTemplate(c *gin.Context) {
	if err := h.svc.DeleteTemplate(c.Param("id")); err != nil {
		writeReportTemplateError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func writeReportTemplateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrReportTemplateNotFound):
		ErrorNotFound(c, err.Error())
	case errors.Is(err, service.ErrReportTemplateNameExists):
		ErrorConflict(c, err.Error())
	case errors.Is(err, service.ErrReportTemplateActive):
		ErrorBadRequest(c, err.Error())
	case strings.Contains(err.Error(), "report API endpoint") || strings.Contains(err.Error(), "name is required"):
		ErrorBadRequest(c, err.Error())
	default:
		ErrorInternal(c, err.Error())
	}
}
