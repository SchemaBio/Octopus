package handler

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
	"time"
)

type ExampleDataHandler struct{ cfg *config.Config }

func NewExampleDataHandler(cfg *config.Config) *ExampleDataHandler {
	return &ExampleDataHandler{cfg: cfg}
}
func (h *ExampleDataHandler) Template(c *gin.Context) {
	if !h.cfg.IsSaaS() {
		ErrorNotFound(c, "not found")
		return
	}
	if _, ok := authenticateMachineCallback(c, h.cfg, 4096); !ok {
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if c.ShouldBindJSON(&req) != nil {
		ErrorBadRequest(c, "invalid request")
		return
	}
	info, err := service.ExampleTemplate(c.Request.Context(), h.cfg, req.Version)
	if err != nil {
		ErrorInternal(c, "template unavailable")
		return
	}
	c.JSON(200, info)
}
func (h *ExampleDataHandler) Enqueue(c *gin.Context) {
	if !h.cfg.IsSaaS() {
		ErrorNotFound(c, "not found")
		return
	}
	if _, ok := authenticateMachineCallback(c, h.cfg, 16<<10); !ok {
		return
	}
	var req struct {
		ID              string    `json:"id"`
		OrgID           string    `json:"org_id"`
		UserID          uint      `json:"user_id"`
		TemplateVersion string    `json:"template_version"`
		ApprovedAt      time.Time `json:"approved_at"`
	}
	if c.ShouldBindJSON(&req) != nil {
		ErrorBadRequest(c, "invalid request")
		return
	}
	grant, err := service.NewExampleDataService(h.cfg).Enqueue(c.Request.Context(), model.ExampleDataGrant{ID: req.ID, OrgID: req.OrgID, UserID: req.UserID, TemplateVersion: req.TemplateVersion, ApprovedAt: req.ApprovedAt})
	if err != nil {
		ErrorConflict(c, "example grant unavailable or conflicting")
		return
	}
	c.JSON(200, grant)
}
func (h *ExampleDataHandler) Status(c *gin.Context) {
	if !h.cfg.IsSaaS() {
		ErrorNotFound(c, "not found")
		return
	}
	org, ok := middleware.GetCurrentOrg(c)
	if !ok {
		ErrorNotFound(c, "organization unavailable")
		return
	}
	var g model.ExampleDataGrant
	if database.DB.WithContext(c.Request.Context()).Where("org_id=?", org).Find(&g).Error != nil {
		ErrorInternal(c, "example status unavailable")
		return
	}
	if g.ID == "" {
		Success(c, gin.H{"status": "not_selected"})
		return
	}
	status := g.Status
	if !time.Now().Before(g.ApprovedAt.Add(720 * time.Hour)) {
		status = "expired"
	}
	Success(c, gin.H{"status": status, "sample_id": g.SampleUUID, "task_id": g.TaskUUID, "data_expires_at": g.ApprovedAt.Add(168 * time.Hour), "bam_expires_at": g.ApprovedAt.Add(168 * time.Hour), "expires_at": g.ApprovedAt.Add(720 * time.Hour)})
}
