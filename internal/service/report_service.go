package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
)

// ReportService handles report business logic
type ReportService struct {
	cfg          *config.Config
	repo         *repository.ReportRepository
	templateRepo *repository.ReportTemplateRepository
	taskRepo     *repository.TaskRepository
}

func NewReportService(cfg *config.Config) *ReportService {
	return &ReportService{
		cfg:          cfg,
		repo:         repository.NewReportRepository(),
		templateRepo: repository.NewReportTemplateRepository(),
		taskRepo:     repository.NewTaskRepository(),
	}
}

// ListByTaskID returns all reports for a task
func (s *ReportService) ListByTaskID(taskID string) ([]model.ReportResponse, error) {
	reports, err := s.repo.FindByTaskID(taskID)
	if err != nil {
		return nil, err
	}

	results := make([]model.ReportResponse, len(reports))
	for i, r := range reports {
		results[i] = r.ToResponse()
	}
	return results, nil
}

// CreateReport triggers report generation via external API
func (s *ReportService) CreateReport(taskID string, req *model.ReportCreateRequest, userID string) (*model.Report, error) {
	// Verify task exists
	task, err := s.taskRepo.FindByUUID(taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	reportUUID := uuid.New().String()

	report := &model.Report{
		ID:        reportUUID,
		TaskID:    taskID,
		Name:      req.Name,
		Type:      "generated",
		Status:    model.ReportStatusDraft,
		CreatedBy: userID,
	}

	// If template specified, call external API
	if req.TemplateName != "" {
		report.TemplateName = req.TemplateName
		tmpl, err := s.templateRepo.FindByName(req.TemplateName)
		if err == nil && tmpl != nil {
			report.Type = "generated"
			// Call external report generation API in background
			go s.callExternalAPI(tmpl, task, report)
		}
	}

	if err := s.repo.Create(report); err != nil {
		return nil, err
	}

	return report, nil
}

// callExternalAPI calls the user's configured report generation API
func (s *ReportService) callExternalAPI(tmpl *model.ReportTemplate, task *model.Task, report *model.Report) {
	// Validate endpoint for SSRF protection
	if err := validateReportAPIEndpoint(tmpl.APIEndpoint); err != nil {
		s.updateReportStatus(report.ID, model.ReportStatusDraft, "")
		return
	}

	payload := map[string]interface{}{
		"reportId":   report.ID,
		"taskId":     task.UUID,
		"taskName":   task.Name,
		"sampleId":   task.SampleID,
		"pipeline":   task.Pipeline,
		"reportName": report.Name,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", tmpl.APIEndpoint, bytes.NewReader(body))
	if err != nil {
		s.updateReportStatus(report.ID, model.ReportStatusDraft, "")
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if tmpl.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+tmpl.APIKey)
	}

	client := reportHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		s.updateReportStatus(report.ID, model.ReportStatusDraft, "")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Parse response for external job ID
		var result map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&result) == nil {
			if jobID, ok := result["jobId"].(string); ok {
				s.updateReportStatus(report.ID, model.ReportStatusPendingReview, jobID)
				return
			}
		}
		s.updateReportStatus(report.ID, model.ReportStatusPendingReview, "")
	}
}

func (s *ReportService) updateReportStatus(reportID string, status model.ReportStatus, externalJobID string) {
	report, err := s.repo.FindByStringID(reportID)
	if err != nil {
		return
	}
	report.Status = status
	if externalJobID != "" {
		report.ExternalJobID = externalJobID
	}
	report.UpdatedAt = time.Now()
	s.repo.Update(report)
}

// ListActiveTemplates returns all active report templates (public-safe, no sensitive fields)
func (s *ReportService) ListActiveTemplates() ([]model.ReportTemplateResponse, error) {
	templates, err := s.templateRepo.FindActive()
	if err != nil {
		return nil, err
	}
	results := make([]model.ReportTemplateResponse, len(templates))
	for i, t := range templates {
		results[i] = t.ToResponse()
	}
	return results, nil
}

// CreateTemplate creates a new report template
func (s *ReportService) CreateTemplate(req *model.ReportTemplateCreateRequest) (*model.ReportTemplate, error) {
	tmpl := &model.ReportTemplate{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		APIEndpoint: req.APIEndpoint,
		APIKey:      req.APIKey,
		IsActive:    true,
	}

	if err := s.templateRepo.Create(tmpl); err != nil {
		return nil, err
	}
	return tmpl, nil
}

// validateReportAPIEndpoint validates a report API endpoint for SSRF protection.
func validateReportAPIEndpoint(rawURL string) error {
	return validateReportEndpointWithResolver(rawURL, net.LookupIP)
}

func validateReportEndpointWithResolver(rawURL string, lookup func(string) ([]net.IP, error)) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid report API endpoint")
	}
	if u.User != nil {
		return fmt.Errorf("report API endpoint must not include user info")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("report API endpoint must use https")
	}

	host := u.Hostname()
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "127.0.0.1") {
		return fmt.Errorf("report API endpoint host is not allowed")
	}

	ips, err := lookup(host)
	if err != nil {
		return fmt.Errorf("failed to resolve report API endpoint host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("report API endpoint host did not resolve")
	}
	for _, ip := range ips {
		if !isPublicReportIP(ip) {
			return fmt.Errorf("report API endpoint must resolve to public IP addresses")
		}
	}
	return nil
}

func isPublicReportIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return true
}

func reportHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: reportHTTPTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return validateReportAPIEndpoint(req.URL.String())
		},
	}
}

func reportHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext: reportDialContext,
	}
}

func reportDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("report API endpoint host did not resolve")
	}
	for _, ip := range ips {
		if !isPublicReportIP(ip.IP) {
			return nil, fmt.Errorf("report API endpoint must resolve to public IP addresses")
		}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
	}
	return nil, fmt.Errorf("report API endpoint must resolve to public IP addresses")
}