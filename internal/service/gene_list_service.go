package service

import (
	"errors"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
)

// GeneListService handles gene list business logic
type GeneListService struct {
	cfg  *config.Config
	repo *repository.GeneListRepository
}

// NewGeneListService creates a new gene list service
func NewGeneListService(cfg *config.Config) *GeneListService {
	return &GeneListService{
		cfg:  cfg,
		repo: repository.NewGeneListRepository(),
	}
}

// List returns paginated gene lists
func (s *GeneListService) List(query *model.GeneListListQuery) (*model.GeneListListResponse, error) {
	lists, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.GeneListResponse, len(lists))
	for i, g := range lists {
		items[i] = g.ToResponse()
	}

	return &model.GeneListListResponse{
		Total: int(total),
		Items: items,
	}, nil
}

// Get returns a single gene list
func (s *GeneListService) Get(id string) (*model.GeneListResponse, error) {
	geneList, err := s.repo.FindByStringID(id)
	if err != nil {
		return nil, errors.New("gene list not found")
	}

	resp := geneList.ToResponse()
	return &resp, nil
}

// Create creates a new gene list
func (s *GeneListService) Create(req *model.GeneListCreateRequest, userID uint) (*model.GeneListResponse, error) {
	if s.repo.ExistsByName(req.Name) {
		return nil, errors.New("gene list name already exists")
	}

	geneList := &model.GeneList{
		ID:              uuid.New().String(),
		Name:            req.Name,
		Description:     req.Description,
		Category:        req.Category,
		DiseaseCategory: req.DiseaseCategory,
		CreatedBy:       userID,
	}
	geneList.SetGenes(req.Genes)

	if err := s.repo.Create(geneList); err != nil {
		return nil, err
	}

	resp := geneList.ToResponse()
	return &resp, nil
}

// Update updates a gene list
func (s *GeneListService) Update(id string, req *model.GeneListUpdateRequest) (*model.GeneListResponse, error) {
	geneList, err := s.repo.FindByStringID(id)
	if err != nil {
		return nil, errors.New("gene list not found")
	}

	if req.Name != "" {
		geneList.Name = req.Name
	}
	if req.Description != "" {
		geneList.Description = req.Description
	}
	if req.Genes != nil {
		geneList.SetGenes(req.Genes)
	}
	if req.Category != "" {
		geneList.Category = req.Category
	}
	if req.DiseaseCategory != "" {
		geneList.DiseaseCategory = req.DiseaseCategory
	}

	if err := s.repo.Update(geneList); err != nil {
		return nil, err
	}

	resp := geneList.ToResponse()
	return &resp, nil
}

// Delete deletes a gene list
func (s *GeneListService) Delete(id string) error {
	return s.repo.DeleteByID(id)
}
