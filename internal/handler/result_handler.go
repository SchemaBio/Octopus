package handler

import (
	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type ResultHandler struct {
	svc *service.ResultService
}

func NewResultHandler(cfg *config.Config) *ResultHandler {
	return &ResultHandler{
		svc: service.NewResultService(cfg),
	}
}

// GetQC returns QC results for a task
func (h *ResultHandler) GetQC(c *gin.Context) {
	taskID := c.Param("id")

	qc, err := h.svc.GetQC(c.Request.Context(), taskID)
	if err != nil {
		ErrorNotFound(c, "QC result not found")
		return
	}

	Success(c, qc)
}

// ListSNVIndels returns paginated SNV/Indel results
func (h *ResultHandler) ListSNVIndels(c *gin.Context) {
	var query model.SNVIndelListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = c.Param("id")
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListSNVIndels(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListCNVSegments returns paginated CNV segment results
func (h *ResultHandler) ListCNVSegments(c *gin.Context) {
	var query model.CNVSegmentListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = c.Param("id")
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListCNVSegments(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListCNVExons returns paginated CNV exon results
func (h *ResultHandler) ListCNVExons(c *gin.Context) {
	var query model.CNVExonListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = c.Param("id")
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListCNVExons(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListSTRs returns paginated STR results
func (h *ResultHandler) ListSTRs(c *gin.Context) {
	var query model.STRListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = c.Param("id")
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListSTRs(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListMEIs returns paginated MEI results
func (h *ResultHandler) ListMEIs(c *gin.Context) {
	var query model.MEIListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = c.Param("id")
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListMEIs(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListMTVariants returns paginated mitochondrial variant results
func (h *ResultHandler) ListMTVariants(c *gin.Context) {
	var query model.MTListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = c.Param("id")
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListMTVariants(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListUPDRegions returns paginated UPD region results
func (h *ResultHandler) ListUPDRegions(c *gin.Context) {
	var query model.UPDListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = c.Param("id")
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListUPDRegions(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ReviewVariant marks a variant as reviewed
func (h *ResultHandler) ReviewVariant(c *gin.Context) {
	taskID := c.Param("id")
	variantType := c.Param("type")
	vid := c.Param("vid")

	_, _, email, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	_ = taskID

	if err := h.svc.ReviewVariant(c.Request.Context(), variantType, vid, email); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, gin.H{"reviewed": true})
}

// ReportVariant marks a variant as reported
func (h *ResultHandler) ReportVariant(c *gin.Context) {
	taskID := c.Param("id")
	variantType := c.Param("type")
	vid := c.Param("vid")

	_, _, email, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	_ = taskID

	if err := h.svc.ReportVariant(c.Request.Context(), variantType, vid, email); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, gin.H{"reported": true})
}

// setQueryDefaults sets default page and pageSize
func setQueryDefaults(page, pageSize *int) {
	if *page == 0 {
		*page = 1
	}
	if *pageSize == 0 {
		*pageSize = 20
	}
}
