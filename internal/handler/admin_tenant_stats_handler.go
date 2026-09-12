package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/gin-gonic/gin"
)

const maxAdminTenantStatsOrgIDs = 100

// AdminTenantStatsHandler exposes the bounded tenant aggregate consumed by
// Cuttlefish. It deliberately lives behind the Octopus admin route and is
// reachable only through Squid's explicit proxy allowlist.
type AdminTenantStatsHandler struct{}

func NewAdminTenantStatsHandler() *AdminTenantStatsHandler {
	return &AdminTenantStatsHandler{}
}

// GetTenantStats aggregates tasks, result-import failures, and upload health
// for the requested tenant IDs in the Octopus database. Every requested ID is
// returned, including tenants with no rows, so callers can distinguish a real
// zero from an omitted group.
func (h *AdminTenantStatsHandler) GetTenantStats(c *gin.Context) {
	orgIDs, err := parseAdminTenantStatsOrgIDs(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	db := database.GetDB()
	if db == nil {
		Error(c, http.StatusServiceUnavailable, "Octopus database is unavailable")
		return
	}

	stats := make(map[string]model.AdminTenantStats, len(orgIDs))
	for _, orgID := range orgIDs {
		stats[orgID] = model.AdminTenantStats{OrgID: orgID}
	}

	const taskTenantExpr = "COALESCE(NULLIF(tasks.external_org_id, ''), NULLIF(regexp_replace(tasks.tenant_id, '^org:', ''), ''))"
	var taskRows []struct {
		OrgID            string
		TaskCount        int64
		FailedTaskCount  int64
		RunningTaskCount int64
	}
	if err := db.WithContext(c.Request.Context()).Table("tasks").
		Select(taskTenantExpr+` AS org_id,
			COUNT(*) AS task_count,
			COUNT(*) FILTER (WHERE tasks.status = ?) AS failed_task_count,
			COUNT(*) FILTER (WHERE tasks.status = ?) AS running_task_count`,
			model.TaskStatusFailed, model.TaskStatusRunning).
		Where("tasks.deleted_at IS NULL").
		Where(taskTenantExpr+" IN ?", orgIDs).
		Group(taskTenantExpr).
		Scan(&taskRows).Error; err != nil {
		ErrorInternal(c, "Failed to load tenant task statistics")
		return
	}
	for _, row := range taskRows {
		item, ok := stats[row.OrgID]
		if !ok {
			continue
		}
		item.TaskCount = row.TaskCount
		item.FailedTaskCount = row.FailedTaskCount
		item.RunningTaskCount = row.RunningTaskCount
		stats[row.OrgID] = item
	}

	// Count a task once when either its current status or any recorded import
	// attempt reports failure. This avoids double-counting retries while still
	// retaining the task-level status used by older Octopus records.
	var importRows []struct {
		OrgID                    string
		ResultImportFailureCount int64
	}
	if err := db.WithContext(c.Request.Context()).Table("tasks").
		Joins("LEFT JOIN result_import_batches ON result_import_batches.task_uuid = tasks.uuid").
		Select(taskTenantExpr+` AS org_id,
			COUNT(DISTINCT tasks.uuid) FILTER (WHERE tasks.result_import_status = ? OR result_import_batches.status = ?) AS result_import_failure_count`,
			model.ResultImportStatusFailed, model.ResultImportBatchStatusFailed).
		Where("tasks.deleted_at IS NULL").
		Where(taskTenantExpr+" IN ?", orgIDs).
		Group(taskTenantExpr).
		Scan(&importRows).Error; err != nil {
		ErrorInternal(c, "Failed to load tenant result-import statistics")
		return
	}
	for _, row := range importRows {
		item, ok := stats[row.OrgID]
		if !ok {
			continue
		}
		item.ResultImportFailureCount = row.ResultImportFailureCount
		stats[row.OrgID] = item
	}

	cutoff := time.Now().Add(-time.Hour)
	var uploadRows []struct {
		OrgID             string
		UploadedFileCount int64
		UploadedBytes     int64
		StaleUploadCount  int64
	}
	if err := db.WithContext(c.Request.Context()).Table("upload_files").
		Joins("JOIN upload_jobs ON upload_jobs.id = upload_files.job_id").
		Select(`upload_jobs.external_org_id AS org_id,
			COUNT(*) FILTER (WHERE upload_files.status <> ?) AS uploaded_file_count,
			COALESCE(SUM(CASE WHEN upload_files.status <> ? THEN upload_files.file_size ELSE 0 END), 0) AS uploaded_bytes,
			COUNT(*) FILTER (WHERE upload_files.status = ? AND upload_files.created_at < ?) AS stale_upload_count`,
			model.FileStatusDeleted, model.FileStatusDeleted, model.FileStatusPending, cutoff).
		Where("upload_jobs.external_org_id IN ?", orgIDs).
		Group("upload_jobs.external_org_id").
		Scan(&uploadRows).Error; err != nil {
		ErrorInternal(c, "Failed to load tenant upload statistics")
		return
	}
	for _, row := range uploadRows {
		item, ok := stats[row.OrgID]
		if !ok {
			continue
		}
		item.UploadedFileCount = row.UploadedFileCount
		item.UploadedBytes = row.UploadedBytes
		item.StaleUploadCount = row.StaleUploadCount
		stats[row.OrgID] = item
	}

	ordered := make([]model.AdminTenantStats, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		ordered = append(ordered, stats[orgID])
	}
	Success(c, ordered)
}

func parseAdminTenantStatsOrgIDs(c *gin.Context) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("tenant IDs are required")
	}
	raw := append([]string{}, c.QueryArray("org_id")...)
	raw = append(raw, c.QueryArray("org_ids")...)
	seen := make(map[string]struct{}, len(raw))
	ids := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			if len(id) > 160 || strings.ContainsAny(id, "\r\n") {
				return nil, fmt.Errorf("invalid tenant ID")
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one tenant ID is required")
	}
	if len(ids) > maxAdminTenantStatsOrgIDs {
		return nil, fmt.Errorf("at most %d tenant IDs may be requested", maxAdminTenantStatsOrgIDs)
	}
	return ids, nil
}
