package service

import (
	"context"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
)

// HistoryService handles history business logic
type HistoryService struct {
	cfg  *config.Config
	repo *repository.HistoryRepository
}

// NewHistoryService creates a new history service
func NewHistoryService(cfg *config.Config) *HistoryService {
	return &HistoryService{
		cfg:  cfg,
		repo: repository.NewHistoryRepository(),
	}
}

func (s *HistoryService) GetGroupedSNVIndels(ctx context.Context, query *model.HistoryListQuery) ([]model.GroupedSNVIndel, int64, error) {
	return s.repo.GetGroupedSNVIndels(query)
}

func (s *HistoryService) GetGroupedCNVSegments(ctx context.Context, query *model.HistoryListQuery) ([]model.GroupedCNVSegment, int64, error) {
	return s.repo.GetGroupedCNVSegments(query)
}

func (s *HistoryService) GetGroupedCNVExons(ctx context.Context, query *model.HistoryListQuery) ([]model.GroupedCNVExon, int64, error) {
	return s.repo.GetGroupedCNVExons(query)
}

func (s *HistoryService) GetGroupedSTRs(ctx context.Context, query *model.HistoryListQuery) ([]model.GroupedSTR, int64, error) {
	return s.repo.GetGroupedSTRs(query)
}

func (s *HistoryService) GetGroupedMEIs(ctx context.Context, query *model.HistoryListQuery) ([]model.GroupedMEI, int64, error) {
	return s.repo.GetGroupedMEIs(query)
}

func (s *HistoryService) GetGroupedMTVariants(ctx context.Context, query *model.HistoryListQuery) ([]model.GroupedMTVariant, int64, error) {
	return s.repo.GetGroupedMTVariants(query)
}

func (s *HistoryService) GetGroupedUPDRegions(ctx context.Context, query *model.HistoryListQuery) ([]model.GroupedUPDRegion, int64, error) {
	return s.repo.GetGroupedUPDRegions(query)
}
