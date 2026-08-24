package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
)

const (
	defaultReportContentType    = "application/octet-stream"
	maxReportDownloadBytes      = 50 << 20
	defaultReportRequestTimeout = 5 * time.Minute
)

var (
	ErrReportDownloadTooLarge   = errors.New("report API response exceeds maximum size")
	ErrReportTemplateNotFound   = errors.New("report template not found")
	ErrReportTemplateNameExists = errors.New("report template name already exists")
	ErrReportTemplateActive     = errors.New("active report template cannot be deleted")
)

// ReportService handles report business logic
type ReportService struct {
	cfg          *config.Config
	repo         *repository.ReportRepository
	templateRepo *repository.ReportTemplateRepository
	packageSvc   *ResultPackageService
	http         *http.Client
}

// ReportDownload is a generated report stream returned directly to the client.
type ReportDownload struct {
	FileName      string
	ContentType   string
	ContentLength int64
	Body          io.ReadCloser
}

func NewReportService(cfg *config.Config) *ReportService {
	timeout := defaultReportRequestTimeout
	if cfg != nil && cfg.Report.RequestTimeout > 0 {
		timeout = cfg.Report.RequestTimeout
	}
	return &ReportService{
		cfg:          cfg,
		repo:         repository.NewReportRepository(),
		templateRepo: repository.NewReportTemplateRepository(),
		packageSvc:   NewResultPackageService(cfg),
		http:         reportHTTPClientWithTimeout(timeout),
	}
}

// ListByTaskID returns legacy persisted reports for a task.
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

// GenerateReportDownload calls the configured report API and returns its file
// response as a stream. Octopus does not store, archive, or later serve reports
// generated through this endpoint.
func (s *ReportService) GenerateReportDownload(ctx context.Context, ownerUserID uint, task *model.Task, req *model.ReportCreateRequest, userEmail string, roles ...string) (*ReportDownload, error) {
	var tmpl *model.ReportTemplate
	var err error
	if templateID := strings.TrimSpace(req.TemplateID); templateID != "" {
		tmpl, err = s.templateRepo.FindActiveByIDAndOwner(templateID, ownerUserID)
	} else if templateName := strings.TrimSpace(req.TemplateName); templateName != "" {
		// Backward-compatible lookup for older clients, scoped to the current user.
		tmpl, err = s.templateRepo.FindActiveByNameAndOwner(templateName, ownerUserID)
	} else {
		return nil, fmt.Errorf("templateId is required")
	}
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, ErrReportTemplateNotFound
	}
	if s.packageSvc != nil {
		isAdmin := len(roles) > 0 && roles[0] == string(model.SystemRoleSuperAdmin)
		if !isAdmin && (task == nil || task.CreatedBy != ownerUserID) {
			return nil, ErrResultPackageNotReady
		}
	}

	return s.generateReportDownload(ctx, tmpl, task, req, userEmail)
}

func (s *ReportService) generateReportDownload(ctx context.Context, tmpl *model.ReportTemplate, task *model.Task, req *model.ReportCreateRequest, userID string) (*ReportDownload, error) {
	if err := validateReportAPIEndpoint(tmpl.APIEndpoint); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tmpl.APIKey) == "" {
		return nil, fmt.Errorf("report API key is required")
	}

	var packageStatus *model.ResultPackageResponse
	if s.packageSvc != nil {
		var packageErr error
		packageStatus, packageErr = s.packageSvc.Status(ctx, task)
		if packageErr != nil {
			return nil, packageErr
		}
		if packageStatus.Status != model.ResultPackageReady || packageStatus.ResultPackageURL == "" {
			if strings.TrimSpace(packageStatus.Error) != "" {
				return nil, fmt.Errorf("%w: %s", ErrResultPackageNotReady, packageStatus.Error)
			}
			return nil, ErrResultPackageNotReady
		}
	}

	requestID := uuid.New().String()
	reportName := strings.TrimSpace(req.Name)
	if reportName == "" {
		reportName = tmpl.Name
	}
	payload := map[string]interface{}{
		"request_id":       requestID,
		"report_id":        requestID,
		"task_result_uuid": task.UUID,
		"task_name":        task.Name,
		"sample_id":        task.SampleID,
		"pipeline":         task.Pipeline,
		"report_name":      reportName,
		"created_by":       userID,
	}
	if packageStatus != nil {
		payload["task_uuid"] = task.UUID
		payload["result_package_url"] = packageStatus.ResultPackageURL
		payload["result_package_filename"] = packageStatus.FileName
		payload["result_package_size_bytes"] = packageStatus.SizeBytes
		payload["result_package_expires_at"] = packageStatus.ExpiresAt
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tmpl.APIEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/octet-stream,application/pdf,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,*/*")
	httpReq.Header.Set("Authorization", "Bearer "+tmpl.APIKey)

	resp, err := s.httpClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("report API request failed: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if strings.TrimSpace(string(msg)) == "" {
			return nil, fmt.Errorf("report API returned %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("report API returned %d", resp.StatusCode)
	}
	if resp.ContentLength > maxReportDownloadBytes {
		resp.Body.Close()
		return nil, fmt.Errorf("report API response exceeds maximum size of %d MB", maxReportDownloadBytes>>20)
	}

	contentType := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(strings.TrimSpace(mediaType), "application/json") {
		resp.Body.Close()
		return nil, fmt.Errorf("report API returned a non-file response")
	}
	if contentType == "" {
		contentType = defaultReportContentType
	}

	return &ReportDownload{
		FileName:      reportDownloadFileName(req.Name, tmpl.Name, task.UUID, resp.Header.Get("Content-Disposition"), contentType),
		ContentType:   contentType,
		ContentLength: resp.ContentLength,
		Body:          newMaxBytesReadCloser(resp.Body, maxReportDownloadBytes),
	}, nil
}

type maxBytesReadCloser struct {
	rc   io.ReadCloser
	max  int64
	read int64
}

func newMaxBytesReadCloser(rc io.ReadCloser, max int64) io.ReadCloser {
	return &maxBytesReadCloser{rc: rc, max: max}
}

func (r *maxBytesReadCloser) Read(p []byte) (int, error) {
	if r.read >= r.max {
		var probe [1]byte
		n, err := r.rc.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("%w of %d MB", ErrReportDownloadTooLarge, r.max>>20)
		}
		return 0, err
	}

	remaining := r.max - r.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.rc.Read(p)
	r.read += int64(n)
	return n, err
}

func (r *maxBytesReadCloser) Close() error {
	return r.rc.Close()
}

func (s *ReportService) httpClient() *http.Client {
	if s.http != nil {
		return s.http
	}
	return reportHTTPClient()
}

// ValidateTemplateEndpoint verifies URL policy, DNS/TLS, and network reachability
// without invoking the report-generation POST operation.
func (s *ReportService) ValidateTemplateEndpoint(ctx context.Context, endpoint, apiKey string) (int, error) {
	endpoint = strings.TrimSpace(endpoint)
	if err := validateReportAPIEndpoint(endpoint); err != nil {
		return 0, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return 0, fmt.Errorf("report API key is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("report API endpoint is unreachable: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return resp.StatusCode, fmt.Errorf("report API rejected the authentication key with status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// ValidateOwnedTemplateEndpoint reuses a saved key when an owner validates an
// existing template. The key never needs to be returned to the browser.
func (s *ReportService) ValidateOwnedTemplateEndpoint(ctx context.Context, ownerUserID uint, templateID, endpoint, apiKey string) (int, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" && strings.TrimSpace(templateID) != "" {
		tmpl, err := s.templateRepo.FindAnyByIDAndOwner(strings.TrimSpace(templateID), ownerUserID)
		if err != nil {
			return 0, err
		}
		if tmpl == nil {
			return 0, ErrReportTemplateNotFound
		}
		apiKey = tmpl.APIKey
	}
	return s.ValidateTemplateEndpoint(ctx, endpoint, apiKey)
}

func reportDownloadFileName(requestName, templateName, taskID, contentDisposition, contentType string) string {
	if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
		if filename := sanitizeReportFileName(params["filename"]); filename != "" {
			return filename
		}
		if filename := sanitizeReportFileName(params["filename*"]); filename != "" {
			return filename
		}
	}

	base := sanitizeReportFileName(requestName)
	if base == "" {
		base = sanitizeReportFileName(templateName)
	}
	if base == "" {
		base = "report-" + taskID
	}
	if path.Ext(base) == "" {
		base += reportFileExtension(contentType)
	}
	return base
}

func sanitizeReportFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == "/" {
		return ""
	}
	return name
}

func reportFileExtension(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "application/pdf":
		return ".pdf"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "text/html":
		return ".html"
	case "text/plain":
		return ".txt"
	default:
		return ".bin"
	}
}

// ListActiveTemplates returns the current user's active report templates.
func (s *ReportService) ListActiveTemplates(ownerUserID uint) ([]model.ReportTemplateResponse, error) {
	templates, err := s.templateRepo.FindActiveByOwner(ownerUserID)
	if err != nil {
		return nil, err
	}
	results := make([]model.ReportTemplateResponse, len(templates))
	for i, t := range templates {
		results[i] = t.ToResponse()
	}
	return results, nil
}

// ListTemplatesForOwner returns a user's templates with endpoint metadata but never the API key.
func (s *ReportService) ListTemplatesForOwner(ownerUserID uint) ([]model.ReportTemplateAdminResponse, error) {
	templates, err := s.templateRepo.FindAllByOwner(ownerUserID)
	if err != nil {
		return nil, err
	}
	results := make([]model.ReportTemplateAdminResponse, len(templates))
	for i, t := range templates {
		results[i] = t.ToAdminResponse()
	}
	return results, nil
}

// CreateTemplate creates a new report template.
func (s *ReportService) CreateTemplate(ownerUserID uint, req *model.ReportTemplateCreateRequest) (*model.ReportTemplateAdminResponse, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.APIEndpoint = strings.TrimSpace(req.APIEndpoint)
	req.APIKey = strings.TrimSpace(req.APIKey)
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.APIKey == "" {
		return nil, fmt.Errorf("report API key is required")
	}
	if err := validateReportAPIEndpoint(req.APIEndpoint); err != nil {
		return nil, err
	}
	if existing, err := s.templateRepo.FindAnyByNameAndOwner(req.Name, ownerUserID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrReportTemplateNameExists
	}

	tmpl := &model.ReportTemplate{
		ID:          uuid.New().String(),
		OwnerUserID: ownerUserID,
		Name:        req.Name,
		Description: req.Description,
		APIEndpoint: req.APIEndpoint,
		APIKey:      req.APIKey,
		IsActive:    true,
	}

	if err := s.templateRepo.Create(tmpl); err != nil {
		return nil, err
	}
	resp := tmpl.ToAdminResponse()
	return &resp, nil
}

// UpdateTemplate updates mutable report template metadata and optionally rotates the API key.
func (s *ReportService) UpdateTemplate(ownerUserID uint, id string, req *model.ReportTemplateUpdateRequest) (*model.ReportTemplateAdminResponse, error) {
	tmpl, err := s.templateRepo.FindAnyByIDAndOwner(strings.TrimSpace(id), ownerUserID)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, ErrReportTemplateNotFound
	}

	name := strings.TrimSpace(req.Name)
	if name != "" && name != tmpl.Name {
		if existing, err := s.templateRepo.FindAnyByNameAndOwner(name, ownerUserID); err != nil {
			return nil, err
		} else if existing != nil && existing.ID != tmpl.ID {
			return nil, ErrReportTemplateNameExists
		}
		tmpl.Name = name
	}
	tmpl.Description = strings.TrimSpace(req.Description)

	endpoint := strings.TrimSpace(req.APIEndpoint)
	if endpoint != "" && endpoint != tmpl.APIEndpoint {
		if err := validateReportAPIEndpoint(endpoint); err != nil {
			return nil, err
		}
		tmpl.APIEndpoint = endpoint
	}
	if apiKey := strings.TrimSpace(req.APIKey); apiKey != "" {
		tmpl.APIKey = apiKey
	}
	if req.IsActive != nil {
		tmpl.IsActive = *req.IsActive
	}

	if err := s.templateRepo.Update(tmpl); err != nil {
		return nil, err
	}
	resp := tmpl.ToAdminResponse()
	return &resp, nil
}

// SetTemplateActive toggles report template availability.
func (s *ReportService) SetTemplateActive(ownerUserID uint, id string, active bool) (*model.ReportTemplateAdminResponse, error) {
	tmpl, err := s.templateRepo.FindAnyByIDAndOwner(strings.TrimSpace(id), ownerUserID)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, ErrReportTemplateNotFound
	}
	tmpl.IsActive = active
	if err := s.templateRepo.Update(tmpl); err != nil {
		return nil, err
	}
	resp := tmpl.ToAdminResponse()
	return &resp, nil
}

// DeleteTemplate deletes an inactive report template.
func (s *ReportService) DeleteTemplate(ownerUserID uint, id string) error {
	tmpl, err := s.templateRepo.FindAnyByIDAndOwner(strings.TrimSpace(id), ownerUserID)
	if err != nil {
		return err
	}
	if tmpl == nil {
		return ErrReportTemplateNotFound
	}
	if tmpl.IsActive {
		return ErrReportTemplateActive
	}
	return s.templateRepo.DeleteByID(tmpl.ID)
}

func validateReportAPIEndpoint(rawURL string) error {
	return validateReportAPIEndpointWithResolver(rawURL, net.LookupIP)
}

func validateReportAPIEndpointWithResolver(rawURL string, lookup func(string) ([]net.IP, error)) error {
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
	if strings.EqualFold(host, "localhost") {
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
		if !isPublicIP(ip) {
			return fmt.Errorf("report API endpoint must resolve to public IP addresses")
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return true
}

func reportHTTPClient() *http.Client {
	return reportHTTPClientWithTimeout(defaultReportRequestTimeout)
}

func reportHTTPClientWithTimeout(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultReportRequestTimeout
	}
	return &http.Client{
		Timeout:   timeout,
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
		if !isPublicIP(ip.IP) {
			return nil, fmt.Errorf("report API endpoint must resolve to public IP addresses")
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
	}
	return nil, fmt.Errorf("report API endpoint must resolve to public IP addresses")
}
