package sepiida

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bioinfo/schema-platform/internal/model"
)

// Client is the Sepiida API client
type Client struct {
	baseURL   string
	queryKey  string
	timeout   time.Duration
}

// NewClient creates a new Sepiida client
func NewClient(baseURL, queryKey string) *Client {
	return &Client{
		baseURL:  baseURL,
		queryKey: queryKey,
		timeout:  30 * time.Second,
	}
}

// SetTimeout sets the HTTP timeout
func (c *Client) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

// doRequest performs an HTTP request with authentication
func (c *Client) doRequest(method, path string) ([]byte, error) {
	client := &http.Client{Timeout: c.timeout}

	url := c.baseURL + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.queryKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sepiida error (status %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// GetWorkflowByUUID queries workflow by UUID
func (c *Client) GetWorkflowByUUID(uuid string) (*model.SepiidaWorkflow, error) {
	body, err := c.doRequest("GET", "/api/v1/workflow?uuid="+uuid)
	if err != nil {
		return nil, err
	}

	var resp model.SepiidaWorkflowResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("sepiida error: %s", resp.Error)
	}

	return &resp.Workflow, nil
}

// GetWorkflowByID queries workflow by ID
func (c *Client) GetWorkflowByID(id string) (*model.SepiidaWorkflow, error) {
	body, err := c.doRequest("GET", "/api/v1/workflow?id="+id)
	if err != nil {
		return nil, err
	}

	var resp model.SepiidaWorkflowResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("sepiida error: %s", resp.Error)
	}

	return &resp.Workflow, nil
}

// GetWorkflowTasks queries tasks for a workflow
func (c *Client) GetWorkflowTasks(workflowID string) ([]model.SepiidaTask, error) {
	body, err := c.doRequest("GET", "/api/v1/workflow/tasks?id="+workflowID)
	if err != nil {
		return nil, err
	}

	var resp model.SepiidaTasksResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Tasks, nil
}

// ListWorkflows lists all workflows
func (c *Client) ListWorkflows() ([]model.SepiidaWorkflow, error) {
	body, err := c.doRequest("GET", "/api/v1/workflows")
	if err != nil {
		return nil, err
	}

	var resp model.SepiidaWorkflowsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Workflows, nil
}

// GetWorkflowWithTasks queries workflow with its tasks
func (c *Client) GetWorkflowWithTasks(uuid string) (*model.SepiidaWorkflow, []model.SepiidaTask, error) {
	workflow, err := c.GetWorkflowByUUID(uuid)
	if err != nil {
		return nil, nil, err
	}

	if workflow == nil {
		return nil, nil, nil
	}

	tasks, err := c.GetWorkflowTasks(workflow.ID)
	if err != nil {
		return workflow, nil, err
	}

	return workflow, tasks, nil
}

// Health checks Sepiida server health
func (c *Client) Health() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(c.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("sepiida health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("sepiida unhealthy (status %d)", resp.StatusCode)
	}

	return nil
}