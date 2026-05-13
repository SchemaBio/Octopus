package handler

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
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

	var req model.UploadJobCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	job, files, presignedURLs, err := h.svc.CreateJob(c.Request.Context(), userID, &req)
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

func (h *UploadHandler) GetJob(c *gin.Context) {
	uuid := c.Param("uuid")

	job, files, err := h.svc.GetJob(c.Request.Context(), uuid)
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
	uuid := c.Param("uuid")

	if err := h.svc.DeleteJob(c.Request.Context(), uuid); err != nil {
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

func (h *UploadHandler) CompleteCOSFile(c *gin.Context) {
	jobUUID := c.Param("uuid")
	fileUUID := c.Param("file_uuid")

	var req model.UploadFileCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req.FileSize = 0
	}

	file, err := h.svc.CompleteCOSFile(c.Request.Context(), jobUUID, fileUUID, req.FileSize)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, model.UploadFileToResponse(file))
}

func (h *UploadHandler) GetDownloadURL(c *gin.Context) {
	fileUUID := c.Param("file_uuid")

	url, err := h.svc.GetFilePresignedGetURL(c.Request.Context(), fileUUID)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, gin.H{"url": url})
}
