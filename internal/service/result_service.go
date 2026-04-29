package service

import (
	"context"
	"fmt"
	"time"

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

// ========== Review/Report by type ==========

// ReviewVariant marks a variant as reviewed by type
func (s *ResultService) ReviewVariant(ctx context.Context, variantType string, id string, reviewer string) error {
	now := time.Now()
	_ = now

	switch variantType {
	case "snv-indel":
		return s.repo.UpdateSNVIndelReview(id, true, reviewer)
	case "cnv-segment":
		return s.repo.UpdateCNVSegmentReview(id, true, reviewer)
	case "cnv-exon":
		return s.repo.UpdateCNVExonReview(id, true, reviewer)
	case "str":
		return s.repo.UpdateSTRReview(id, true, reviewer)
	case "mei":
		return s.repo.UpdateMEIReview(id, true, reviewer)
	case "mt":
		return s.repo.UpdateMTVariantReview(id, true, reviewer)
	case "upd":
		return s.repo.UpdateUPDRegionReview(id, true, reviewer)
	default:
		return fmt.Errorf("unknown variant type: %s", variantType)
	}
}

// ReportVariant marks a variant as reported by type
func (s *ResultService) ReportVariant(ctx context.Context, variantType string, id string, reporter string) error {
	switch variantType {
	case "snv-indel":
		return s.repo.UpdateSNVIndelReport(id, true, reporter)
	case "cnv-segment":
		return s.repo.UpdateCNVSegmentReport(id, true, reporter)
	case "cnv-exon":
		return s.repo.UpdateCNVExonReport(id, true, reporter)
	case "str":
		return s.repo.UpdateSTRReport(id, true, reporter)
	case "mei":
		return s.repo.UpdateMEIReport(id, true, reporter)
	case "mt":
		return s.repo.UpdateMTVariantReport(id, true, reporter)
	case "upd":
		return s.repo.UpdateUPDRegionReport(id, true, reporter)
	default:
		return fmt.Errorf("unknown variant type: %s", variantType)
	}
}
