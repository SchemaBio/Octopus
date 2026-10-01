package handler

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ResultHandler struct {
	svc       *service.ResultService
	taskRepo  *repository.TaskRepository
	eventRepo *repository.VariantReviewEventRepository
}

func executionAttemptID(task *model.Task) string {
	if task == nil || task.ExecutionAttemptID == "" {
		if task == nil {
			return ""
		}
		return task.UUID
	}
	return task.ExecutionAttemptID
}

func isStableResultRowID(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func NewResultHandler(cfg *config.Config) *ResultHandler {
	return &ResultHandler{
		svc:       service.NewResultService(cfg),
		taskRepo:  repository.NewTaskRepository(),
		eventRepo: repository.NewVariantReviewEventRepository(),
	}
}

// GetContext returns a single attempt-scoped context for the interpretation
// workspace. It is deliberately separate from the paginated result endpoints
// so browsers can discard their cache when an execution is re-imported.
func (h *ResultHandler) GetContext(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	context, err := h.svc.GetContext(c.Request.Context(), task)
	if err != nil {
		ErrorInternal(c, "failed to load result context")
		return
	}
	Success(c, context)
}

func (h *ResultHandler) QueryParquetTable(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	var query model.ParquetQueryRequest
	if err := c.ShouldBindJSON(&query); err != nil {
		ErrorBadRequest(c, "invalid table query")
		return
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 200 || query.Offset < 0 || len(query.Filters) > 40 || len(query.Search) > 256 || len(query.Sort) > 256 {
		ErrorBadRequest(c, "table query exceeds its limits")
		return
	}
	result, err := h.svc.QueryParquetTable(c.Request.Context(), task, c.Param("table"), query)
	if errors.Is(err, service.ErrParquetIncomplete) {
		ErrorConflict(c, "归档 Parquet 与原始报告行数不一致，需要恢复数据集")
		return
	}
	if err != nil {
		ErrorInternal(c, "failed to query the attempt Parquet dataset")
		return
	}
	Success(c, result)
}

func (h *ResultHandler) SaveParquetRowAdjustment(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	_, email, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	var request model.ResultRowAdjustmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorBadRequest(c, "invalid result adjustment")
		return
	}
	if request.DatasetVersion == "" || request.AttemptID == "" {
		ErrorBadRequest(c, "datasetVersion and attemptId are required")
		return
	}
	item, event, err := h.svc.SaveParquetRowAdjustment(c.Request.Context(), task, c.Param("table"), c.Param("vid"), email, request)
	if errors.Is(err, service.ErrAdjustmentConflict) {
		ErrorConflict(c, "result adjustment changed; reload the row")
		return
	}
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, gin.H{"adjustment": item, "event": event})
}

func (h *ResultHandler) ListParquetRowHistory(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	rows, err := h.svc.ListParquetRowAdjustmentEvents(c.Request.Context(), task, c.Param("table"), c.Param("vid"))
	if err != nil {
		ErrorBadRequest(c, "invalid result history query")
		return
	}
	Success(c, rows)
}

func (h *ResultHandler) GetParquetRowAdjustment(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	item, err := h.svc.GetParquetRowAdjustment(c.Request.Context(), task, c.Param("table"), c.Param("vid"))
	if err != nil {
		ErrorBadRequest(c, "invalid result row identity")
		return
	}
	var payload json.RawMessage = json.RawMessage(`{}`)
	if item.PayloadJSON != "" {
		payload = json.RawMessage(item.PayloadJSON)
	}
	Success(c, gin.H{"version": item.Version, "adjustments": payload})
}

// GetIGVSession returns safe evidence descriptors. Object keys and short-lived
// URLs are intentionally omitted from this metadata response.
func (h *ResultHandler) GetIGVSession(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	session, err := h.svc.GetIGVSession(c.Request.Context(), task)
	if err != nil {
		ErrorInternal(c, "failed to load IGV evidence")
		return
	}
	Success(c, session)
}

// SignIGVTrackURLs signs only manifest-authorized tracks for this task. A
// version conflict tells the browser to refresh its result context first.
func (h *ResultHandler) SignIGVTrackURLs(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	var request model.IGVURLRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorBadRequest(c, "trackIds and version are required")
		return
	}
	urls, err := h.svc.SignIGVTracks(c.Request.Context(), task, request)
	if err != nil {
		if errors.Is(err, service.ErrIGVEvidenceChanged) {
			ErrorConflict(c, "result evidence changed; refresh the result context")
			return
		}
		ErrorBadRequest(c, "requested IGV evidence is unavailable")
		return
	}
	Success(c, urls)
}

// ListReviewEvents lists the audit timeline for one authorized task.
func (h *ResultHandler) ListReviewEvents(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	_, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	q := &model.VariantReviewEventListQuery{TaskUUID: task.UUID, Page: 1, PageSize: 50}
	if role == string(model.SystemRoleSuperAdmin) {
		q.IncludeAll = true
	} else {
		q.TenantID = model.TenantIDForTask(task)
	}
	q.VariantType = c.Query("variant_type")
	q.VariantFingerprint = c.Query("variant_fingerprint")
	q.ExecutionAttemptID = c.Query("execution_attempt_id")
	if value := c.Query("page"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &q.Page)
	}
	if value := c.Query("page_size"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &q.PageSize)
	}
	rows, total, err := h.eventRepo.List(q)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	items := make([]model.VariantReviewEventResponse, len(rows))
	for i := range rows {
		items[i] = rows[i].ToResponse()
	}
	SuccessList(c, items, total, q.Page, q.PageSize)
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
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}
	var query model.SNVIndelListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	query.TenantID = model.TenantIDForTask(task)
	query.ExecutionAttemptID = executionAttemptID(task)
	setQueryDefaults(&query.Page, &query.PageSize)

	results, total, err := h.svc.ListSNVIndels(c.Request.Context(), &query, taskActorFromContext(c))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ErrorNotFound(c, "Gene list not found")
			return
		}
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, results, total, query.Page, query.PageSize)
}

// ListCNVSegments returns paginated CNV segment results
func (h *ResultHandler) ListCNVSegments(c *gin.Context) {
	taskID := c.Param("id")
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}
	var query model.CNVSegmentListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	query.TenantID = model.TenantIDForTask(task)
	query.ExecutionAttemptID = executionAttemptID(task)
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
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}
	var query model.CNVExonListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	query.TenantID = model.TenantIDForTask(task)
	query.ExecutionAttemptID = executionAttemptID(task)
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
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}
	var query model.STRListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	query.TenantID = model.TenantIDForTask(task)
	query.ExecutionAttemptID = executionAttemptID(task)
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
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}
	var query model.MEIListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	query.TenantID = model.TenantIDForTask(task)
	query.ExecutionAttemptID = executionAttemptID(task)
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
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}
	var query model.MTListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	query.TenantID = model.TenantIDForTask(task)
	query.ExecutionAttemptID = executionAttemptID(task)
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
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}
	var query model.UPDListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	query.TenantID = model.TenantIDForTask(task)
	query.ExecutionAttemptID = executionAttemptID(task)
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
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}
	var query model.ROHListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	query.TaskID = taskID
	query.TenantID = model.TenantIDForTask(task)
	query.ExecutionAttemptID = executionAttemptID(task)
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

	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}

	userID, email, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	var request struct {
		Reviewed *bool `json:"reviewed" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Reviewed == nil {
		ErrorBadRequest(c, "reviewed must be explicitly provided")
		return
	}
	if isStableResultRowID(vid) {
		ErrorBadRequest(c, "use the versioned result row adjustment endpoint")
		return
	}

	mutation, err := h.svc.ReviewVariantWithEvent(c.Request.Context(), task, variantType, vid, *request.Reviewed, userID, email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ErrorNotFound(c, "Result not found")
			return
		}
		ErrorBadRequest(c, err.Error())
		return
	}

	response := gin.H{"reviewed": mutation.Reviewed, "changed": mutation.Changed}
	if mutation.ReviewedAt != nil {
		response["reviewedAt"] = mutation.ReviewedAt.UTC().Format(time.RFC3339)
	}
	if mutation.ReviewedBy != "" {
		response["reviewedBy"] = mutation.ReviewedBy
	}
	if mutation.Event != nil {
		response["event"] = mutation.Event.ToResponse()
	}
	Success(c, response)
}

// ReportVariant marks a variant as reported
func (h *ResultHandler) ReportVariant(c *gin.Context) {
	taskID := c.Param("id")
	variantType := c.Param("type")
	vid := c.Param("vid")

	_, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}

	_, email, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	if isStableResultRowID(vid) {
		ErrorBadRequest(c, "use the versioned result row adjustment endpoint")
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
	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
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
	result, err := h.svc.SaveCNVAssessmentScoped(task, variantType, variantID, req.Assessment, email)
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

func (h *ResultHandler) ExportParquetTable(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	var query model.ParquetQueryRequest
	if c.ShouldBindJSON(&query) != nil || len(query.Filters) > 40 || len(query.Search) > 256 || len(query.Sort) > 256 || query.DatasetVersion == "" || query.AttemptID == "" {
		ErrorBadRequest(c, "invalid export query or missing dataset snapshot")
		return
	}
	resp, err := h.svc.ExportParquetTable(c.Request.Context(), task, c.Param("table"), query)
	if errors.Is(err, service.ErrAdjustmentConflict) {
		ErrorConflict(c, "dataset changed; refresh before exporting")
		return
	}
	if err != nil {
		ErrorInternal(c, "effective result export failed")
		return
	}
	defer resp.Body.Close()
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=results-%s.csv", c.Param("table")))
	c.DataFromReader(200, resp.ContentLength, "text/csv; charset=utf-8", io.LimitReader(resp.Body, 1<<30), nil)
}
