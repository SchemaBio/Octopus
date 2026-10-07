package handler

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func historyTenant(c *gin.Context) (string, bool) {
	user, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return "", false
	}
	org, _ := middleware.GetCurrentOrg(c)
	// Admins use the same explicit JWT organization scope as other users.
	return model.TenantIDForIdentity(org, user), true
}

func (h *HistoryHandler) SyncReports(c *gin.Context) {
	tenant, ok := historyTenant(c)
	if !ok {
		return
	}
	var watermark *uint64
	if value := c.Query("watermark"); value != "" {
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			ErrorBadRequest(c, "invalid watermark")
			return
		}
		watermark = &n
	}
	limit := 1000
	if value := c.Query("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 1000 {
			ErrorBadRequest(c, "invalid sync page size")
			return
		}
		limit = n
	}
	result, err := service.SyncHistoryReports(c.Request.Context(), tenant, c.Query("table"), c.Query("cursor"), watermark, limit)
	if errors.Is(err, service.ErrAdjustmentConflict) {
		Error(c, 410, "HISTORY_CURSOR_EXPIRED")
		return
	}
	if err != nil {
		ErrorBadRequest(c, "history synchronization unavailable or invalid cursor")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	encoded, marshalErr := json.Marshal(gin.H{"data": result})
	if marshalErr != nil {
		ErrorInternal(c, "history synchronization encoding failed")
		return
	}
	c.Header("Vary", "Accept-Encoding")
	c.Header("Content-Type", "application/json; charset=utf-8")
	accept := strings.ReplaceAll(c.GetHeader("Accept-Encoding"), " ", "")
	if len(encoded) > 1024 && strings.Contains(accept, "gzip") && !strings.Contains(accept, "gzip;q=0") {
		c.Header("Content-Encoding", "gzip")
		writer := gzip.NewWriter(c.Writer)
		_, _ = writer.Write(encoded)
		_ = writer.Close()
	} else {
		_, _ = c.Writer.Write(encoded)
	}
}

func (h *HistoryHandler) ReportDetail(c *gin.Context) {
	tenant, ok := historyTenant(c)
	if !ok {
		return
	}
	row, events, err := service.HistoryReportDetail(c.Request.Context(), tenant, c.Param("id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ErrorNotFound(c, "reported source not found")
		return
	}
	if err != nil {
		ErrorInternal(c, "history detail unavailable")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	Success(c, gin.H{"row": row, "events": events, "source": gin.H{"datasetId": row.DatasetID, "datasetVersion": row.DatasetVersion, "rowId": row.RowID, "rowOrdinal": row.RowOrdinal}})
}
