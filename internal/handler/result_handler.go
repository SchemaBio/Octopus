package handler

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

type ResultHandler struct {
	svc      *service.ResultService
	taskRepo *repository.TaskRepository
}

func NewResultHandler(cfg *config.Config) *ResultHandler {
	return &ResultHandler{
		svc:      service.NewResultService(cfg),
		taskRepo: repository.NewTaskRepository(),
	}
}

// GetQC returns QC results for a task
func (h *ResultHandler) GetQC(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	qc, err := h.svc.GetQC(c.Request.Context(), taskID)
	if err != nil {
		ErrorNotFound(c, "QC result not found")
		return
	}

	Success(c, qc)
}

// ListSNVIndels returns paginated SNV/Indel results
func (h *ResultHandler) ListSNVIndels(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.SNVIndelListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
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
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.CNVSegmentListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
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
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.CNVExonListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
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
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.STRListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
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
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.MEIListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
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
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.MTListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
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
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.UPDListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListUPDRegions(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListROHRegions returns paginated ROH region results
func (h *ResultHandler) ListROHRegions(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.ROHListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListROHRegions(c.Request.Context(), &query)
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

	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	_, email, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	if err := h.svc.ReviewVariant(c.Request.Context(), variantType, taskID, vid, email); err != nil {
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

	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	_, email, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	if err := h.svc.ReportVariant(c.Request.Context(), variantType, taskID, vid, email); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	Success(c, gin.H{"reported": true})
}

// ListCNVAssessments returns saved ClinGen CNV assessments for a task/type.
func (h *ResultHandler) ListCNVAssessments(c *gin.Context) {
	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	var query model.CNVAssessmentListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	results, err := h.svc.ListCNVAssessments(taskID, query.VariantType, query.VariantIDs)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, results)
}

// GetCNVAssessment returns one saved ClinGen CNV assessment.
func (h *ResultHandler) GetCNVAssessment(c *gin.Context) {
	taskID := c.Param("id")
	variantType := c.Param("type")
	variantID := c.Param("vid")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	result, err := h.svc.GetCNVAssessment(taskID, variantType, variantID)
	if err != nil {
		ErrorNotFound(c, "CNV assessment not found")
		return
	}
	Success(c, result)
}

// SaveCNVAssessment stores one ClinGen CNV assessment payload.
func (h *ResultHandler) SaveCNVAssessment(c *gin.Context) {
	taskID := c.Param("id")
	variantType := c.Param("type")
	variantID := c.Param("vid")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}
	_, email, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	var req model.CNVAssessmentUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	result, err := h.svc.SaveCNVAssessment(taskID, variantType, variantID, req.Assessment, email)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, result)
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
