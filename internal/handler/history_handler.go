package handler

import (
	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type HistoryHandler struct {
	svc *service.HistoryService
}

func NewHistoryHandler(cfg *config.Config) *HistoryHandler {
	return &HistoryHandler{
		svc: service.NewHistoryService(cfg),
	}
}

func (h *HistoryHandler) bindQuery(c *gin.Context) *model.HistoryListQuery {
	var query model.HistoryListQuery
	c.ShouldBindQuery(&query)
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	return &query
}

// ListGroupedSNVIndels returns grouped SNV/Indel history
func (h *HistoryHandler) ListGroupedSNVIndels(c *gin.Context) {
	query := h.bindQuery(c)
	results, total, err := h.svc.GetGroupedSNVIndels(c.Request.Context(), query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListGroupedCNVSegments returns grouped CNV segment history
func (h *HistoryHandler) ListGroupedCNVSegments(c *gin.Context) {
	query := h.bindQuery(c)
	results, total, err := h.svc.GetGroupedCNVSegments(c.Request.Context(), query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListGroupedCNVExons returns grouped CNV exon history
func (h *HistoryHandler) ListGroupedCNVExons(c *gin.Context) {
	query := h.bindQuery(c)
	results, total, err := h.svc.GetGroupedCNVExons(c.Request.Context(), query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListGroupedSTRs returns grouped STR history
func (h *HistoryHandler) ListGroupedSTRs(c *gin.Context) {
	query := h.bindQuery(c)
	results, total, err := h.svc.GetGroupedSTRs(c.Request.Context(), query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListGroupedMEIs returns grouped MEI history
func (h *HistoryHandler) ListGroupedMEIs(c *gin.Context) {
	query := h.bindQuery(c)
	results, total, err := h.svc.GetGroupedMEIs(c.Request.Context(), query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListGroupedMTVariants returns grouped MT variant history
func (h *HistoryHandler) ListGroupedMTVariants(c *gin.Context) {
	query := h.bindQuery(c)
	results, total, err := h.svc.GetGroupedMTVariants(c.Request.Context(), query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListGroupedUPDRegions returns grouped UPD region history
func (h *HistoryHandler) ListGroupedUPDRegions(c *gin.Context) {
	query := h.bindQuery(c)
	results, total, err := h.svc.GetGroupedUPDRegions(c.Request.Context(), query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, results, total, query.Page, query.PageSize)
}
