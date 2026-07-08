package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
)

// ResultService handles result business logic
type ResultService struct {
	cfg  *config.Config
	repo *repository.ResultRepository
}

// NewResultService creates a new result service
func NewResultService(cfg *config.Config) *ResultService {
	return &ResultService{
		cfg:  cfg,
		repo: repository.NewResultRepository(),
	}
}

// ========== QC ==========

func (s *ResultService) GetQC(ctx context.Context, taskID string) (*model.QCResult, error) {
	return s.repo.FindQCByTaskID(taskID)
}

// ========== SNV/Indel ==========

func (s *ResultService) ListSNVIndels(ctx context.Context, query *model.SNVIndelListQuery) ([]model.SNVIndel, int64, error) {
	return s.repo.PaginateSNVIndels(query)
}

func (s *ResultService) ReviewSNVIndel(ctx context.Context, id string, reviewer string) error {
	return s.repo.UpdateSNVIndelReview(id, true, reviewer)
}

func (s *ResultService) ReportSNVIndel(ctx context.Context, id string, reporter string) error {
	return s.repo.UpdateSNVIndelReport(id, true, reporter)
}

// ========== CNV Segment ==========

func (s *ResultService) ListCNVSegments(ctx context.Context, query *model.CNVSegmentListQuery) ([]model.CNVSegment, int64, error) {
	return s.repo.PaginateCNVSegments(query)
}

func (s *ResultService) ReviewCNVSegment(ctx context.Context, id string, reviewer string) error {
	return s.repo.UpdateCNVSegmentReview(id, true, reviewer)
}

func (s *ResultService) ReportCNVSegment(ctx context.Context, id string, reporter string) error {
	return s.repo.UpdateCNVSegmentReport(id, true, reporter)
}

// ========== CNV Exon ==========

func (s *ResultService) ListCNVExons(ctx context.Context, query *model.CNVExonListQuery) ([]model.CNVExon, int64, error) {
	return s.repo.PaginateCNVExons(query)
}

func (s *ResultService) ReviewCNVExon(ctx context.Context, id string, reviewer string) error {
	return s.repo.UpdateCNVExonReview(id, true, reviewer)
}

func (s *ResultService) ReportCNVExon(ctx context.Context, id string, reporter string) error {
	return s.repo.UpdateCNVExonReport(id, true, reporter)
}

// ========== STR ==========

func (s *ResultService) ListSTRs(ctx context.Context, query *model.STRListQuery) ([]model.STR, int64, error) {
	return s.repo.PaginateSTRs(query)
}

func (s *ResultService) ReviewSTR(ctx context.Context, id string, reviewer string) error {
	return s.repo.UpdateSTRReview(id, true, reviewer)
}

func (s *ResultService) ReportSTR(ctx context.Context, id string, reporter string) error {
	return s.repo.UpdateSTRReport(id, true, reporter)
}

// ========== MEI ==========

func (s *ResultService) ListMEIs(ctx context.Context, query *model.MEIListQuery) ([]model.MEIVariant, int64, error) {
	return s.repo.PaginateMEIs(query)
}

func (s *ResultService) ReviewMEI(ctx context.Context, id string, reviewer string) error {
	return s.repo.UpdateMEIReview(id, true, reviewer)
}

func (s *ResultService) ReportMEI(ctx context.Context, id string, reporter string) error {
	return s.repo.UpdateMEIReport(id, true, reporter)
}

// ========== Mitochondrial ==========

func (s *ResultService) ListMTVariants(ctx context.Context, query *model.MTListQuery) ([]model.MitochondrialVariant, int64, error) {
	return s.repo.PaginateMTVariants(query)
}

func (s *ResultService) ReviewMTVariant(ctx context.Context, id string, reviewer string) error {
	return s.repo.UpdateMTVariantReview(id, true, reviewer)
}

func (s *ResultService) ReportMTVariant(ctx context.Context, id string, reporter string) error {
	return s.repo.UpdateMTVariantReport(id, true, reporter)
}

// ========== UPD ==========

func (s *ResultService) ListUPDRegions(ctx context.Context, query *model.UPDListQuery) ([]model.UPDRegion, int64, error) {
	return s.repo.PaginateUPDRegions(query)
}

func (s *ResultService) ReviewUPDRegion(ctx context.Context, id string, reviewer string) error {
	return s.repo.UpdateUPDRegionReview(id, true, reviewer)
}

func (s *ResultService) ReportUPDRegion(ctx context.Context, id string, reporter string) error {
	return s.repo.UpdateUPDRegionReport(id, true, reporter)
}

// ========== ROH ==========

func (s *ResultService) ListROHRegions(ctx context.Context, query *model.ROHListQuery) ([]model.ROHRegion, int64, error) {
	return s.repo.PaginateROHRegions(query)
}

// ========== Review/Report by type ==========

// ReviewVariant marks a variant as reviewed by type
func (s *ResultService) ReviewVariant(ctx context.Context, variantType string, taskID string, id string, reviewer string) error {
	return s.repo.UpdateVariantReview(variantType, taskID, id, true, reviewer)
}

// ReportVariant marks a variant as reported by type
func (s *ResultService) ReportVariant(ctx context.Context, variantType string, taskID string, id string, reporter string) error {
	return s.repo.UpdateVariantReport(variantType, taskID, id, true, reporter)
}

// ========== CNV assessment persistence ==========

const maxCNVAssessmentPayloadBytes = 64 * 1024

func isCNVAssessmentType(variantType string) bool {
	return variantType == "cnv-segment" || variantType == "cnv-exon"
}

func validateCNVAssessmentPayload(payload json.RawMessage, variantID string) error {
	if len(payload) == 0 {
		return errors.New("assessment is required")
	}
	if len(payload) > maxCNVAssessmentPayloadBytes {
		return errors.New("assessment payload is too large")
	}
	if !utf8.Valid(payload) {
		return errors.New("assessment payload must be valid UTF-8 JSON")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return errors.New("assessment payload must be a JSON object")
	}
	cnvID, _ := parsed["cnvId"].(string)
	if cnvID == "" || cnvID != variantID {
		return errors.New("assessment cnvId does not match variant ID")
	}
	if _, ok := parsed["criteria"]; !ok {
		return errors.New("assessment criteria is required")
	}
	classification, ok := parsed["classification"].(string)
	if !ok {
		return errors.New("assessment classification is required")
	}
	switch classification {
	case "Pathogenic", "Likely_Pathogenic", "VUS", "Likely_Benign", "Benign":
	default:
		return errors.New("assessment classification is invalid")
	}
	return nil
}

func (s *ResultService) ListCNVAssessments(taskID, variantType, idsCSV string) ([]model.CNVAssessmentResponse, error) {
	if !isCNVAssessmentType(variantType) {
		return nil, errors.New("unsupported CNV assessment type")
	}
	ids := splitCSV(idsCSV)
	rows, err := s.repo.ListCNVAssessments(taskID, variantType, ids)
	if err != nil {
		return nil, err
	}
	out := make([]model.CNVAssessmentResponse, len(rows))
	for i := range rows {
		out[i] = model.CNVAssessmentToResponse(&rows[i])
	}
	return out, nil
}

func (s *ResultService) GetCNVAssessment(taskID, variantType, variantID string) (*model.CNVAssessmentResponse, error) {
	if !isCNVAssessmentType(variantType) {
		return nil, errors.New("unsupported CNV assessment type")
	}
	row, err := s.repo.FindCNVAssessment(taskID, variantType, variantID)
	if err != nil {
		return nil, err
	}
	resp := model.CNVAssessmentToResponse(row)
	return &resp, nil
}

func (s *ResultService) SaveCNVAssessment(taskID, variantType, variantID string, payload json.RawMessage, actor string) (*model.CNVAssessmentResponse, error) {
	if !isCNVAssessmentType(variantType) {
		return nil, errors.New("unsupported CNV assessment type")
	}
	if err := validateCNVAssessmentPayload(payload, variantID); err != nil {
		return nil, err
	}
	exists, err := s.repo.VariantExists(variantType, taskID, variantID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("CNV variant not found")
	}
	row, err := s.repo.UpsertCNVAssessment(taskID, variantType, variantID, string(payload), actor)
	if err != nil {
		return nil, err
	}
	resp := model.CNVAssessmentToResponse(row)
	return &resp, nil
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
