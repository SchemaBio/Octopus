package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *ResultHandler) GetIGVReference(c *gin.Context) {
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	stream, err := h.svc.OpenIGVReference(c.Request.Context(), task, c.Param("asset"), c.Query("attempt"), c.GetHeader("Range"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIGVEvidenceChanged):
			Error(c, http.StatusConflict, "IGV execution changed; refresh evidence")
		case errors.Is(err, service.ErrIGVReferenceRange):
			Error(c, http.StatusRequestedRangeNotSatisfiable, "FASTA requires a single bounded byte range")
		default:
			Error(c, http.StatusServiceUnavailable, "reference resource unavailable")
		}
		return
	}
	defer stream.Body.Close()
	c.Header("Content-Type", "text/plain; charset=utf-8")
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
