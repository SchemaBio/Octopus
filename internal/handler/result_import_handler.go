package handler

import (
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/gin-gonic/gin"
)

// ResultImportHandler exposes result-import-batch audit data for monitoring
// consumers (e.g. Cuttlefish). The batches live in the Octopus DB (not Squid);
// this endpoint is reached through the Squid /api/v1/octopus/* proxy.
type ResultImportHandler struct {
	batchRepo *repository.ResultImportBatchRepository
}

func NewResultImportHandler(cfg *config.Config) *ResultImportHandler {
	_ = cfg // reserved for future config-driven behavior (e.g. feature flags)
	return &ResultImportHandler{
		batchRepo: repository.NewResultImportBatchRepository(),
	}
}

// ListBatches returns a paginated list of result-import batches with optional
// status/since filters. Cross-org for SUPER_ADMIN (reachable via
// applyExternalAuth mapping); org-scoped otherwise via the tasks join.
func (h *ResultImportHandler) ListBatches(c *gin.Context) {
	var q model.ResultImportBatchListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 10
	}

	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	// Scope via the query (mirrors applyTaskListScope): SUPER_ADMIN (reachable
	// through applyExternalAuth mapping) sees all; org users see their org's
	// tasks' batches via the tasks join; otherwise own tasks' batches.
	if role == string(model.SystemRoleSuperAdmin) {
		q.IncludeAll = true
	} else if orgID, hasOrg := middleware.GetCurrentOrg(c); hasOrg && orgID != "" {
		q.ExternalOrgID = orgID
	} else {
		q.UserID = userID
	}

	rows, total, err := h.batchRepo.PaginateByQuery(&q)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	items := make([]model.ResultImportBatchResponse, len(rows))
	for i, row := range rows {
		items[i] = model.ResultImportBatchResponse{
			ID:          row.ID,
			TaskUUID:    row.TaskUUID,
			Source:      row.Source,
			Status:      row.Status,
			Fingerprint: row.Fingerprint,
			Error:       row.Error,
			StartedAt:   row.StartedAt.Format(time.RFC3339),
			OrgID:       row.OrgID,
		}
		if row.FinishedAt != nil {
			items[i].FinishedAt = row.FinishedAt.Format(time.RFC3339)
		}
	}

	SuccessList(c, items, total, q.Page, q.PageSize)
}
