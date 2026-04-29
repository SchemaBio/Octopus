package handler

import (
	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type ReportHandler struct {
	svc *service.ReportService
}

func NewReportHandler(cfg *config.Config) *ReportHandler {
	return &ReportHandler{
		svc: service.NewReportService(cfg),
	}
}

// ListReports returns all reports for a task
func (h *ReportHandler) ListReports(c *gin.Context) {
	taskID := c.Param("id")

	reports, err := h.svc.ListByTaskID(taskID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, reports)
}

// CreateReport triggers report generation
func (h *ReportHandler) CreateReport(c *gin.Context) {
	taskID := c.Param("id")

	var req model.ReportCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	_, _, email, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	report, err := h.svc.CreateReport(taskID, &req, email)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	SuccessCreated(c, report.ToResponse())
}

// ListTemplates returns active report templates
func (h *ReportHandler) ListTemplates(c *gin.Context) {
	templates, err := h.svc.ListActiveTemplates()
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, templates)
}

// CreateTemplate creates a new report template
func (h *ReportHandler) CreateTemplate(c *gin.Context) {
	var req model.ReportTemplateCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	tmpl, err := h.svc.CreateTemplate(&req)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessCreated(c, tmpl)
}
