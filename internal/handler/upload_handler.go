package handler

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

type UploadHandler struct {
	svc *service.UploadService
}

func NewUploadHandler(cfg *config.Config) *UploadHandler {
	return &UploadHandler{
		svc: service.NewUploadService(cfg),
	}
}

func (h *UploadHandler) CreateJob(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	orgID, _ := middleware.GetCurrentOrg(c)

	var req model.UploadJobCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	job, files, presignedURLs, err := h.svc.CreateJob(c.Request.Context(), userID, orgID, &req)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	fileResponses := make([]model.UploadFileResponse, len(files))
	for i, file := range files {
		fileResponses[i] = model.UploadFileToResponse(file)
		if i < len(presignedURLs) && presignedURLs[i] != "" {
			fileResponses[i].PresignedURL = presignedURLs[i]
		}
	}

	jobResponse := model.UploadJobToResponse(job, fileResponses)
	SuccessCreated(c, jobResponse)
}

func (h *UploadHandler) ListJobs(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	var query model.UploadJobListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 10
	}

	jobs, total, err := h.svc.ListJobs(c.Request.Context(), userID, &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	items := make([]model.UploadJobResponse, len(jobs))
	for i, job := range jobs {
		files, _ := h.svc.GetJobFiles(c.Request.Context(), job.ID)
		fileResponses := make([]model.UploadFileResponse, len(files))
		for j := range files {
			fileResponses[j] = model.UploadFileToResponse(&files[j])
		}
		items[i] = model.UploadJobToResponse(&job, fileResponses)
	}

	SuccessList(c, items, total, query.Page, query.PageSize)
}

// ListFiles returns the file-level audit list for Cuttlefish (storage_path,
// file_size, org_id exposed for admin/monitoring only). Cross-org for
// SUPER_ADMIN (reachable via applyExternalAuth mapping); org-scoped otherwise.
func (h *UploadHandler) ListFiles(c *gin.Context) {
	var query model.UploadFileListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 10
	}

	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	if role == string(model.SystemRoleSuperAdmin) {
		query.IncludeAll = true
	} else if orgID, hasOrg := middleware.GetCurrentOrg(c); hasOrg && orgID != "" {
		query.ExternalOrgID = orgID
	} else {
		query.UserID = userID
	}

	resp, err := h.svc.ListFiles(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// GetFileStats returns total file count and total uploaded bytes (completed
// files) under the same scope. Separate from ListFiles because SuccessList's
// fixed envelope cannot carry total_bytes without polluting all list endpoints.
func (h *UploadHandler) GetFileStats(c *gin.Context) {
	var query model.UploadFileListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	if role == string(model.SystemRoleSuperAdmin) {
		query.IncludeAll = true
	} else if orgID, hasOrg := middleware.GetCurrentOrg(c); hasOrg && orgID != "" {
		query.ExternalOrgID = orgID
	} else {
		query.UserID = userID
	}

	total, bytes, err := h.svc.GetFileStats(c.Request.Context(), &query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}
	Success(c, gin.H{"total": total, "total_bytes": bytes})
}

func (h *UploadHandler) GetJob(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	uuid := c.Param("uuid")

	job, files, err := h.svc.GetJob(c.Request.Context(), userID, uuid)
	if err != nil {
		ErrorNotFound(c, "Upload job not found")
		return
	}

	fileResponses := make([]model.UploadFileResponse, len(files))
	for i := range files {
		fileResponses[i] = model.UploadFileToResponse(&files[i])
	}

	jobResponse := model.UploadJobToResponse(job, fileResponses)
	Success(c, jobResponse)
}

func (h *UploadHandler) DeleteJob(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	uuid := c.Param("uuid")

	if err := h.svc.DeleteJob(c.Request.Context(), userID, uuid); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *UploadHandler) UploadLocal(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	fileUUID := c.Param("file_uuid")
	if h.svc.Config().Storage.MaxSizeMB > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(h.svc.Config().Storage.MaxSizeMB)*1024*1024)
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		ErrorBadRequest(c, "file field is required: "+err.Error())
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		ErrorInternal(c, "Failed to read uploaded file: "+err.Error())
		return
	}
	defer f.Close()
	if h.svc.Config().Storage.MaxSizeMB > 0 && fileHeader.Size > int64(h.svc.Config().Storage.MaxSizeMB)*1024*1024 {
		ErrorBadRequest(c, fmt.Sprintf("file exceeds maximum size of %d MB", h.svc.Config().Storage.MaxSizeMB))
		return
	}

	uploadFile, err := h.svc.SaveLocalFile(
		c.Request.Context(),
		userID,
		fileUUID,
		f,
		fileHeader.Size,
	)
	if err != nil {
		ErrorInternal(c, "Failed to save file: "+err.Error())
		return
	}

	SuccessCreated(c, model.UploadFileToResponse(uploadFile))
}

func (h *UploadHandler) CompleteS3(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	file, err := h.svc.CompleteS3File(c.Request.Context(), userID, c.Param("file_uuid"))
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, model.UploadFileToResponse(file))
}

func (h *UploadHandler) GetDownloadURL(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	fileUUID := c.Param("file_uuid")

	path, err := h.svc.GetLocalFilePath(c.Request.Context(), userID, fileUUID)
	if err != nil {
		ErrorNotFound(c, "Upload file not found")
		return
	}

	c.FileAttachment(path, filepath.Base(path))
}
