package handler

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type AIHandler struct {
	cfg       *config.Config
	evaluator *service.AIEvaluator
}

func NewAIHandler(cfg *config.Config) *AIHandler {
	return &AIHandler{
		cfg:       cfg,
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

// ProxyAgent proxies LLM API calls from the frontend page-agent to OpenAI.
// The frontend sends requests to /api/v1/ai/proxy/... and the backend forwards
// them to the configured LLM endpoint with the real API key.
// This keeps the API key server-side only.
func (h *AIHandler) ProxyAgent(c *gin.Context) {
	if h.cfg.LLM.BaseURL == "" || h.cfg.LLM.APIKey == "" {
		ErrorBadRequest(c, "LLM is not configured")
		return
	}

	// Strip the proxy prefix to get the target path
	// e.g. /api/v1/ai/proxy/v1/chat/completions → /v1/chat/completions
	targetPath := strings.TrimPrefix(c.Request.URL.Path, "/api/v1/ai/proxy")
	if targetPath == "" {
		targetPath = "/"
	}

	targetURL := strings.TrimRight(h.cfg.LLM.BaseURL, "/") + targetPath
	if c.Request.URL.RawQuery != "" {
		targetURL += "?" + c.Request.URL.RawQuery
	}

	// Read request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		ErrorBadRequest(c, "Failed to read request body")
		return
	}

	// Forward to LLM provider
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, strings.NewReader(string(body)))
	if err != nil {
		ErrorInternal(c, "Failed to create proxy request")
		return
	}

	// Copy relevant headers (Content-Type, Accept, etc.)
	for key, values := range c.Request.Header {
		lower := strings.ToLower(key)
		if lower == "content-type" || lower == "accept" || lower == "accept-encoding" {
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
	}

	// Add the real API key (server-side, never exposed to frontend)
	req.Header.Set("Authorization", "Bearer "+h.cfg.LLM.APIKey)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		ErrorInternal(c, "LLM request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, v := range values {
			c.Header(key, v)
		}
	}

	// Stream response back to frontend
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
