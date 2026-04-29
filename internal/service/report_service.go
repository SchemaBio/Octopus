package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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

	client := &http.Client{Timeout: 30 * time.Second}
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

// ListActiveTemplates returns all active report templates
func (s *ReportService) ListActiveTemplates() ([]model.ReportTemplate, error) {
	return s.templateRepo.FindActive()
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

