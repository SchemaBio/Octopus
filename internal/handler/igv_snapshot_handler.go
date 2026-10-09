package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *ResultHandler) GetIGVSnapshot(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	item, err := h.svc.GetIGVSnapshot(c.Request.Context(), task, c.Query("locus"))
	if err != nil {
		ErrorBadRequest(c, "无法读取该位点的 IGV 截图")
		return
	}
	Success(c, item)
}

func (h *ResultHandler) SaveIGVSnapshot(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxIGVSnapshotBytes+(64<<10))
	file, err := c.FormFile("image")
	if err != nil {
		ErrorBadRequest(c, "需要 PNG 截图")
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	reader, err := file.Open()
	if err != nil {
		ErrorBadRequest(c, "无法读取截图")
		return
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, service.MaxIGVSnapshotBytes+1))
	if err != nil {
		ErrorBadRequest(c, "无法读取截图")
		return
	}
	item, err := h.svc.SaveIGVSnapshot(c.Request.Context(), task, c.PostForm("locus"), c.PostForm("version"), data)
	if errors.Is(err, service.ErrIGVEvidenceChanged) {
		ErrorConflict(c, "测序证据已变更，请刷新后重试")
		return
	}
	if err != nil {
		ErrorBadRequest(c, "IGV 截图保存失败")
		return
	}
	Success(c, item)
}
