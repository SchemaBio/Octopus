package handler

import (
	"fmt"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type AIHandler struct {
	evaluator *service.AIEvaluator
}

func NewAIHandler(cfg *config.Config) *AIHandler {
	return &AIHandler{
		evaluator: service.NewAIEvaluator(cfg),
	}
}

// Evaluate streams or returns an AI evaluation of a task's results
func (h *AIHandler) Evaluate(c *gin.Context) {
	if !h.evaluator.IsEnabled() {
		ErrorBadRequest(c, "AI evaluation is not configured")
		return
	}

	taskID := c.Param("id")

	// Parse optional filter from request body
	filter := service.DefaultAIFilter()
	c.ShouldBindJSON(&filter)

	// Check if client wants streaming
	accept := c.GetHeader("Accept")
	if accept == "text/event-stream" {
		h.evaluateStream(c, taskID, filter)
	} else {
		h.evaluateSync(c, taskID, filter)
	}
}

func (h *AIHandler) evaluateStream(c *gin.Context, taskID string, filter service.AIFilter) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(interface{ Flush() })
	if !ok {
		ErrorInternal(c, "Streaming not supported")
		return
	}

	err := h.evaluator.Evaluate(c.Request.Context(), taskID, filter, func(chunk string) error {
		fmt.Fprintf(c.Writer, "data: %s\n\n", chunk)
		flusher.Flush()
		return nil
	})

	if err != nil {
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
	}

	fmt.Fprintf(c.Writer, "event: done\ndata: \n\n")
	flusher.Flush()
}

func (h *AIHandler) evaluateSync(c *gin.Context, taskID string, filter service.AIFilter) {
	result, err := h.evaluator.EvaluateSync(c.Request.Context(), taskID, filter)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	Success(c, gin.H{"evaluation": result})
}
