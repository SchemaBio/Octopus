package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

// OverlayHTTPError preserves the status returned by the trusted overlay. It
// lets the API layer expose actionable 402/409/502/503 responses instead of
// collapsing every admission/dispatch failure into a generic 400/500.
type OverlayHTTPError struct {
	Status int
	Body   string
}

func (e *OverlayHTTPError) Error() string {
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("overlay returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("overlay returned HTTP %d: %s", e.Status, strings.TrimSpace(e.Body))
}

// OverlayDeniedError represents a valid admission response whose policy
// decision was negative (for example insufficient credits or concurrency).
type OverlayDeniedError struct {
	Action string
	Reason string
}

func (e *OverlayDeniedError) Error() string {
	return fmt.Sprintf("overlay denied task %s: %s", e.Action, e.Reason)
}

// OverlayDispatchOutcomeUnknown reports whether no HTTP response was received
// for a dispatch request. In that case Squid/Tencent may have accepted the
// request, so callers must retain the same attempt and avoid a duplicate
// instance request.
func OverlayDispatchOutcomeUnknown(err error) bool {
	var transport *OverlayTransportError
	return errors.As(err, &transport)
}

type OverlayTransportError struct{ Err error }

func (e *OverlayTransportError) Error() string {
	return fmt.Sprintf("overlay transport error: %v", e.Err)
}
func (e *OverlayTransportError) Unwrap() error { return e.Err }

type OverlayClient struct {
	cfg            config.OverlayConfig
	client         *http.Client
	dispatchClient *http.Client
}

func NewOverlayClient(cfg config.OverlayConfig) *OverlayClient {
	if !cfg.Enabled || strings.TrimSpace(cfg.BaseURL) == "" {
		return nil
	}
	dispatchTimeout := cfg.DispatchTimeout
	if dispatchTimeout <= 0 {
		dispatchTimeout = 30 * time.Second
	}
	return &OverlayClient{
		cfg:            cfg,
		client:         &http.Client{Timeout: cfg.Timeout},
		dispatchClient: &http.Client{Timeout: dispatchTimeout},
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
		return &OverlayHTTPError{Status: status, Body: strings.TrimSpace(string(body))}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if !resp.Allowed {
		if resp.Reason == "" {
			resp.Reason = "request denied by overlay policy"
		}
		return &OverlayDeniedError{Action: req.Action, Reason: resp.Reason}
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

func (c *OverlayClient) DispatchCVMTask(ctx context.Context, req model.CVMDispatchRequest) (*model.CVMDispatchResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("CVM dispatch requires an enabled overlay")
	}
	var response model.CVMDispatchResponse
	status, body, err := c.postJSONWithClient(ctx, c.dispatchClient, c.cfg.TaskDispatchPath, req, &response)
	if err != nil {
		return nil, &OverlayTransportError{Err: fmt.Errorf("CVM dispatch failed: %w", err)}
	}
	if status < 200 || status >= 300 {
		return nil, &OverlayHTTPError{Status: status, Body: strings.TrimSpace(string(body))}
	}
	// Squid may accept a dispatch while spot capacity is unavailable.  The
	// attempt remains durable in Squid and will be retried there; Octopus keeps
	// the task queued until a later state event supplies the instance ID.
	if response.Accepted && response.InstanceID == "" {
		state := strings.ToUpper(strings.TrimSpace(response.InstanceState))
		if state == "WAITING_CAPACITY" || state == "DISPATCHING" {
			if req.AttemptID != "" && response.AttemptID != "" && response.AttemptID != req.AttemptID {
				return nil, fmt.Errorf("CVM dispatch returned a mismatched attempt_id")
			}
			return &response, nil
		}
	}
	if !response.Accepted || response.InstanceID == "" {
		if response.Reason == "" {
			response.Reason = "CVM dispatch was not accepted"
		}
		return nil, fmt.Errorf("%s", response.Reason)
	}
	if req.AttemptID != "" && response.AttemptID != req.AttemptID {
		return nil, fmt.Errorf("CVM dispatch returned a mismatched attempt_id")
	}
	return &response, nil
}

func (c *OverlayClient) CancelCVMTask(ctx context.Context, req model.CVMCancelRequest) error {
	if c == nil {
		return nil
	}
	status, body, err := c.postJSON(ctx, c.cfg.TaskCancelPath, req, nil)
	if err != nil {
		return fmt.Errorf("cancel CVM task failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("cancel CVM task returned %d: %s", status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *OverlayClient) ChargeCredits(ctx context.Context, req model.OverlayCreditChargeRequest) (*model.OverlayCreditResponse, error) {
	if c == nil {
		return &model.OverlayCreditResponse{Allowed: true}, nil
	}
	var resp model.OverlayCreditResponse
	status, body, err := c.postJSON(ctx, "/api/v1/overlay/credits/charge", req, &resp)
	if err != nil {
		return nil, fmt.Errorf("overlay credit charge failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("overlay credit charge returned %d: %s", status, strings.TrimSpace(string(body)))
	}
	if !resp.Allowed {
		if resp.Reason == "" {
			resp.Reason = "credit charge denied"
		}
		return nil, fmt.Errorf("%s", resp.Reason)
	}
	return &resp, nil
}

func (c *OverlayClient) RefundCredits(ctx context.Context, req model.OverlayCreditRefundRequest) error {
	if c == nil {
		return nil
	}
	status, body, err := c.postJSON(ctx, "/api/v1/overlay/credits/refund", req, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("overlay credit refund returned %d: %s", status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *OverlayClient) postJSON(ctx context.Context, path string, payload interface{}, out interface{}) (int, []byte, error) {
	return c.postJSONWithClient(ctx, c.client, path, payload, out)
}

func (c *OverlayClient) postJSONWithClient(ctx context.Context, client *http.Client, path string, payload interface{}, out interface{}) (int, []byte, error) {
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

	resp, err := client.Do(req)
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
