package handler

import (
	"errors"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
)

func (h *ResultHandler) GetLocalIGVSnapshotFile(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	filename, err := h.svc.LocalIGVSnapshotFile(task, c.Query("locus"), c.Query("attempt"), c.Query("version"))
	if err != nil {
		if errors.Is(err, service.ErrIGVEvidenceChanged) {
			Error(c, http.StatusConflict, "IGV execution changed; refresh evidence")
		} else {
			ErrorNotFound(c, "IGV snapshot unavailable")
		}
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.File(filename)
}

func (h *ResultHandler) GetLocalIGVTrack(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	stream, err := h.svc.OpenLocalIGVTrack(task, c.Param("track"), c.Param("asset"), c.Query("attempt"), c.Query("version"), c.GetHeader("Range"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIGVEvidenceChanged):
			Error(c, http.StatusConflict, "IGV execution changed; refresh evidence")
		case errors.Is(err, service.ErrIGVReferenceRange):
			Error(c, http.StatusRequestedRangeNotSatisfiable, "invalid IGV byte range")
		default:
			ErrorNotFound(c, "IGV track unavailable")
		}
		return
	}
	defer stream.Body.Close()
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(stream.Length, 10))
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if stream.ContentRange != "" {
		c.Header("Content-Range", stream.ContentRange)
		c.Status(http.StatusPartialContent)
	} else {
		c.Status(http.StatusOK)
	}
	_, _ = io.CopyN(c.Writer, stream.Body, stream.Length)
}
