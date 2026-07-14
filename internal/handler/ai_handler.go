package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

const defaultAIProxyMaxBodyBytes = 2 << 20 // 2 MB

type AIHandler struct {
	cfg       *config.Config
	evaluator *service.AIEvaluator
	taskRepo  *repository.TaskRepository
	http      *http.Client
	validate  func(string) error
}

func NewAIHandler(cfg *config.Config) *AIHandler {
	return &AIHandler{
		cfg:       cfg,
		evaluator: service.NewAIEvaluator(cfg),
		taskRepo:  repository.NewTaskRepository(),
		http:      aiProxyHTTPClient(),
		validate:  validateAIProxyEndpoint,
	}
}

// Evaluate streams or returns an AI evaluation of a task's results
func (h *AIHandler) Evaluate(c *gin.Context) {
	if !h.evaluator.IsEnabled() {
		ErrorBadRequest(c, "AI evaluation is not configured")
		return
	}

	taskID := c.Param("id")
	if _, ok := requireTaskAccess(c, h.taskRepo, taskID); !ok {
		return
	}

	// Parse optional filter from request body
	filter := service.DefaultAIFilter()
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&filter); err != nil && err != io.EOF {
			ErrorBadRequest(c, err.Error())
			return
		}
	}

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

	// Only allow POST method
	if c.Request.Method != http.MethodPost {
		ErrorBadRequest(c, "unsupported LLM proxy method")
		return
	}

	// Strip the proxy prefix to get the target path
	// e.g. /api/v1/ai/proxy/v1/chat/completions → /v1/chat/completions
	targetPath := c.Param("path")
	if targetPath == "" {
		targetPath = strings.TrimPrefix(c.Request.URL.Path, "/api/v1/ai/proxy")
	}
	if targetPath == "" {
		targetPath = "/"
	}
	targetPath = strings.TrimPrefix(targetPath, "/v1")

	// Path whitelist
	if targetPath != "/chat/completions" && targetPath != "/responses" {
		ErrorBadRequest(c, "unsupported LLM proxy path")
		return
	}

	// Validate and construct target URL
	baseURL, err := url.Parse(strings.TrimRight(h.cfg.LLM.BaseURL, "/"))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		ErrorInternal(c, "Invalid LLM base URL")
		return
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + targetPath
	baseURL.RawQuery = ""
	targetURL := baseURL.String()

	// Validate endpoint (HTTPS, public IP, no localhost)
	if err := h.validateAIProxyTarget(targetURL); err != nil {
		ErrorInternal(c, "Invalid LLM base URL")
		return
	}

	// Read request body with size limit
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, h.maxProxyBodyBytes()))
	if err != nil {
		Error(c, http.StatusRequestEntityTooLarge, "LLM proxy request body is too large")
		return
	}

	// Extract and validate model
	modelName, err := extractAIProxyModel(body)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if !isAIProxyModelAllowed(modelName, h.cfg.LLM.AllowedModels) {
		Error(c, http.StatusForbidden, "LLM model is not allowed")
		return
	}

	// Forward to LLM provider
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		ErrorInternal(c, "Failed to create proxy request")
		return
	}

	// Copy only headers needed by OpenAI-compatible JSON/SSE APIs
	for key, values := range c.Request.Header {
		lower := strings.ToLower(key)
		if lower == "content-type" || lower == "accept" {
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
	}

	// Add the real API key (server-side, never exposed to frontend)
	req.Header.Set("Authorization", "Bearer "+h.cfg.LLM.APIKey)
	req.Header.Set("Accept-Encoding", "identity")

	// Send request using reusable HTTP client
	resp, err := h.httpClient().Do(req)
	if err != nil {
		ErrorInternal(c, "LLM request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// Copy only safe response headers
	for _, key := range []string{"Content-Type", "Cache-Control"} {
		if v := resp.Header.Get(key); v != "" {
			c.Header(key, v)
		}
	}

	// Stream response back to frontend
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}

func (h *AIHandler) maxProxyBodyBytes() int64 {
	if h.cfg.LLM.ProxyMaxBodyBytes <= 0 {
		return defaultAIProxyMaxBodyBytes
	}
	return h.cfg.LLM.ProxyMaxBodyBytes
}

func (h *AIHandler) httpClient() *http.Client {
	if h.http != nil {
		return h.http
	}
	return aiProxyHTTPClient()
}

func (h *AIHandler) validateAIProxyTarget(targetURL string) error {
	if h.validate != nil {
		return h.validate(targetURL)
	}
	return validateAIProxyEndpoint(targetURL)
}

func validateAIProxyEndpoint(rawURL string) error {
	return validateAIProxyEndpointWithResolver(rawURL, net.LookupIP)
}

func validateAIProxyEndpointWithResolver(rawURL string, lookup func(string) ([]net.IP, error)) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid LLM base URL")
	}
	if u.User != nil {
		return fmt.Errorf("LLM base URL must not include user info")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("LLM base URL must use https")
	}

	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("LLM base URL host is not allowed")
	}

	ips, err := lookup(host)
	if err != nil {
		return fmt.Errorf("failed to resolve LLM base URL host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("LLM base URL host did not resolve")
	}
	for _, ip := range ips {
		if !isPublicAIProxyIP(ip) {
			return fmt.Errorf("LLM base URL must resolve to public IP addresses")
		}
	}
	return nil
}

func isPublicAIProxyIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return true
}

func aiProxyHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: aiProxyHTTPTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return validateAIProxyEndpoint(req.URL.String())
		},
	}
}

func aiProxyHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext: aiProxyDialContext,
	}
}

func aiProxyDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("LLM base URL host did not resolve")
	}
	for _, ip := range ips {
		if !isPublicAIProxyIP(ip.IP) {
			return nil, fmt.Errorf("LLM base URL must resolve to public IP addresses")
		}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
	}
	return nil, fmt.Errorf("LLM base URL must resolve to public IP addresses")
}

func extractAIProxyModel(body []byte) (string, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("LLM proxy request body must be valid JSON")
	}

	rawModel, ok := payload["model"]
	if !ok {
		return "", fmt.Errorf("LLM proxy request body must include model")
	}

	var modelName string
	if err := json.Unmarshal(rawModel, &modelName); err != nil || strings.TrimSpace(modelName) == "" {
		return "", fmt.Errorf("LLM proxy request model must be a non-empty string")
	}
	return strings.TrimSpace(modelName), nil
}

func isAIProxyModelAllowed(modelName string, allowedModels []string) bool {
	if len(allowedModels) == 0 {
		return true
	}
	for _, allowed := range allowedModels {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || allowed == modelName {
			return true
		}
	}
	return false
}
