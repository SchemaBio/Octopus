package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"gorm.io/gorm"
)

import "github.com/gin-gonic/gin"

func (h *ResultDownloadHandler) RequestNetwork(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	task, ok := requireTaskAccess(c, h.tasks, c.Param("id"))
	if !ok {
		return
	}
	var body struct {
		GrantID string `json:"grant_id"`
	}
	if c.ShouldBindJSON(&body) != nil || body.GrantID == "" {
		ErrorBadRequest(c, "download_grant_not_found")
		return
	}
	request, err := h.svc.RequestNetwork(c.Request.Context(), task, taskActorFromContext(c), body.GrantID, downloadClientIP(c))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, request)
}

type DownloadProtectionHandler struct {
	cfg *config.Config
	svc *service.ResultDownloadService
}

func NewDownloadProtectionHandler(cfg *config.Config) *DownloadProtectionHandler {
	return &DownloadProtectionHandler{cfg: cfg, svc: service.NewDownloadProtectionAdminService(cfg)}
}
func (h *DownloadProtectionHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		ErrorBadRequest(c, "invalid_page")
		return
	}
	size := 50
	db := database.DB.WithContext(c.Request.Context()).Model(&model.ResultDownload{}).Where("protection_version > 0")
	for _, filter := range []struct{ key, column string }{{"org_id", "org_id"}, {"user_id", "user_id"}, {"state", "issuance_state"}} {
		if value := c.Query(filter.key); value != "" {
			if len(value) > 100 {
				ErrorBadRequest(c, "invalid_filter")
				return
			}
			if filter.key == "user_id" {
				if id, err := strconv.ParseUint(value, 10, 64); err != nil || id == 0 {
					ErrorBadRequest(c, "invalid_filter")
					return
				}
			}
			if filter.key == "state" && value != "active" && value != "threshold" && value != "paused" && value != "rebind_wait" && value != "network_pending" {
				ErrorBadRequest(c, "invalid_filter")
				return
			}
			if filter.key == "state" && value == "network_pending" {
				db = db.Where("EXISTS (SELECT 1 FROM download_network_requests n WHERE n.grant_id=result_downloads.id AND n.status IN ('pending','approved_wait'))")
			} else {
				db = db.Where(filter.column+" = ?", value)
			}
		}
	}
	var total int64
	var rows []model.ResultDownload
	if err := db.Count(&total).Error; err != nil {
		ErrorInternal(c, "download_list_unavailable")
		return
	}
	if err := db.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		ErrorInternal(c, "download_list_unavailable")
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if len(ids) > 0 {
		var requests []model.DownloadNetworkRequest
		if err := database.DB.WithContext(c.Request.Context()).Where("grant_id IN ?", ids).Order("created_at DESC").Find(&requests).Error; err != nil {
			ErrorInternal(c, "download_list_unavailable")
			return
		}
		latest := map[string]model.DownloadNetworkRequest{}
		for _, request := range requests {
			if _, ok := latest[request.GrantID]; !ok {
				latest[request.GrantID] = request
			}
		}
		for i := range rows {
			if request, ok := latest[rows[i].ID]; ok {
				rows[i].NetworkRequest = &request
			}
		}
	}
	// Explicit admin-only identity fields; the user API continues hiding them.
	items := make([]map[string]interface{}, 0, len(rows))
	for _, q := range rows {
		items = append(items, map[string]interface{}{"grant": q, "org_id": q.OrgID, "user_id": q.UserID, "task_id": q.TaskUUID})
	}
	var health model.DownloadUsageSync
	err := database.DB.WithContext(c.Request.Context()).First(&health, "id = ?", "cos").Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		ErrorInternal(c, "download_usage_unavailable")
		return
	}
	Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "sync": health, "enabled": h.cfg.Storage.DownloadProtectionEnabled, "log_configured": h.cfg.Storage.DownloadCLSTopicID != ""})
}
func (h *DownloadProtectionHandler) Detail(c *gin.Context) {
	detail, err := service.DownloadProtectionDetailFor(c.Request.Context(), c.Param("grant"))
	if err != nil {
		ErrorNotFound(c, "download_grant_not_found")
		return
	}
	Success(c, detail)
}
func (h *DownloadProtectionHandler) Action(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var body struct {
		Revision  *uint64 `json:"revision"`
		RequestID string  `json:"request_id"`
		Reason    string  `json:"reason"`
	}
	if c.ShouldBindJSON(&body) != nil || body.Revision == nil {
		ErrorBadRequest(c, "download_action_invalid")
		return
	}
	actor, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "unauthorized")
		return
	}
	err := h.svc.ManageProtection(c.Request.Context(), c.Param("grant"), c.Param("action"), body.RequestID, strings.TrimSpace(body.Reason), *body.Revision, actor)
	if errors.Is(err, service.ErrDownloadRevision) {
		Error(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		ErrorBadRequest(c, "download_action_failed")
		return
	}
	h.Detail(c)
}
func (h *DownloadProtectionHandler) Sync(c *gin.Context) {
	actor, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "unauthorized")
		return
	}
	err := service.SyncDownloadUsage(c.Request.Context(), h.cfg)
	if auditErr := service.RecordDownloadSyncAction(c.Request.Context(), actor, err == nil); auditErr != nil {
		ErrorInternal(c, "download_audit_unavailable")
		return
	}
	if err != nil {
		Error(c, http.StatusServiceUnavailable, "download_usage_sync_failed")
		return
	}
	Success(c, gin.H{"synced": true})
}
