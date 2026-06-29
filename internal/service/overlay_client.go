package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
)

type OverlayClient struct {
	cfg    config.OverlayConfig
	client *http.Client
}

func NewOverlayClient(cfg config.OverlayConfig) *OverlayClient {
	if !cfg.Enabled || strings.TrimSpace(cfg.BaseURL) == "" {
		return nil
	}
	return &OverlayClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (c *OverlayClient) AdmitTask(ctx context.Context, req model.OverlayTaskAdmissionRequest) error {
	if c == nil {
		return nil
	}

	var resp model.OverlayTaskAdmissionResponse
	status, body, err := c.postJSON(ctx, c.cfg.TaskAdmissionPath, req, &resp)
	if err != nil {
		if c.cfg.FailOpen {
			fmt.Printf("WARNING: overlay task admission failed open: %v\n", err)
			return nil
		}
		return fmt.Errorf("overlay task admission failed: %w", err)
	}
	if status < 200 || status >= 300 {
		if c.cfg.FailOpen {
			fmt.Printf("WARNING: overlay task admission returned %d and failed open: %s\n", status, strings.TrimSpace(string(body)))
			return nil
		}
		return fmt.Errorf("overlay task admission returned %d: %s", status, strings.TrimSpace(string(body)))
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if !resp.Allowed {
		if resp.Reason == "" {
			resp.Reason = "request denied by overlay policy"
		}
		return fmt.Errorf("overlay denied task %s: %s", req.Action, resp.Reason)
	}
	return nil
}

func (c *OverlayClient) EmitTaskEvent(ctx context.Context, req model.OverlayTaskEventRequest) error {
	if c == nil {
		return nil
	}
	status, body, err := c.postJSON(ctx, c.cfg.TaskEventPath, req, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("overlay task event returned %d: %s", status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *OverlayClient) postJSON(ctx context.Context, path string, payload interface{}, out interface{}) (int, []byte, error) {
	endpoint, err := joinOverlayURL(c.cfg.BaseURL, path)
	if err != nil {
		return 0, nil, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.SharedSecret != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.SharedSecret)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	if out != nil && len(bytes.TrimSpace(respBody)) > 0 && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return resp.StatusCode, respBody, err
		}
	}
	return resp.StatusCode, respBody, nil
}

func joinOverlayURL(baseURL, endpointPath string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return "", fmt.Errorf("unsupported overlay URL scheme %q", base.Scheme)
	}
	pathURL, err := url.Parse(endpointPath)
	if err != nil {
		return "", err
	}
	if pathURL.IsAbs() {
		return pathURL.String(), nil
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(endpointPath, "/")
	return base.String(), nil
}
