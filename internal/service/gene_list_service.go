package service

import (
	"errors"
	"github.com/SchemaBio/Octopus/internal/database"
	"gorm.io/gorm"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
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
		items[i] = geneListResponse(&g, model.OverlayActor{UserID: query.CreatedBy, OrgID: query.OrgID, Role: query.ActorRole, OrgRole: query.ActorOrgRole})
	}

	return &model.GeneListListResponse{
		Total: total,
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

func (s *GeneListService) GetModel(id string) (*model.GeneList, error) {
	geneList, err := s.repo.FindByStringID(id)
	if err != nil {
		return nil, errors.New("gene list not found")
	}
	return geneList, nil
}

// Create creates a new gene list
func (s *GeneListService) Create(req *model.GeneListCreateRequest, userID uint, actors ...model.OverlayActor) (*model.GeneListResponse, error) {
	a := model.OverlayActor{UserID: userID}
	if len(actors) > 0 {
		a = actors[0]
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Genes = normalizedGenes(req.Genes)
	if req.Name == "" || len(req.Name) > 200 || len(req.Genes) == 0 || len(req.Genes) > 50000 {
		return nil, errors.New("name and non-empty gene list are required (maximum 50000 genes)")
	}

	geneList := &model.GeneList{
		ID: uuid.New().String(), ExternalOrgID: a.OrgID, Revision: 1,
		Name:            req.Name,
		Description:     req.Description,
		Category:        req.Category,
		DiseaseCategory: req.DiseaseCategory,
		CreatedBy:       userID,
	}
	geneList.SetGenes(req.Genes)

	if err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.GeneList{}).Where("external_org_id=? AND (? <> '' OR created_by=?) AND lower(name)=lower(?)", a.OrgID, a.OrgID, a.UserID, req.Name).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("gene list name already exists")
		}
		return tx.Create(geneList).Error
	}); err != nil {
		return nil, err
	}

	resp := geneListResponse(geneList, a)
	return &resp, nil
}

// Update updates a gene list
func (s *GeneListService) Update(id string, req *model.GeneListUpdateRequest, actors ...model.OverlayActor) (*model.GeneListResponse, error) {
	a := model.OverlayActor{}
	if len(actors) > 0 {
		a = actors[0]
	}
	g, err := s.repo.FindScopedByStringID(id, a)
	if err != nil {
		return nil, errors.New("gene list not found")
	}
	if !a.ResourceMaintenance(g.CreatedBy) {
		return nil, ErrResourceForbidden
	}
	if req.ExpectedRevision != g.Revision {
		return nil, ErrResourceConflict
	}
	if req.Name != "" {
		g.Name = strings.TrimSpace(req.Name)
	}
	if req.Description != nil {
		g.Description = strings.TrimSpace(*req.Description)
	}
	if req.DiseaseCategory != nil {
		g.DiseaseCategory = strings.TrimSpace(*req.DiseaseCategory)
	}
	if req.Genes != nil {
		v := normalizedGenes(req.Genes)
		if len(v) == 0 || len(v) > 50000 {
			return nil, errors.New("non-empty gene list required (maximum 50000 genes)")
		}
		g.SetGenes(v)
	}
	if req.Category != "" {
		g.Category = req.Category
	}
	if g.Name == "" || len(g.Name) > 200 {
		return nil, errors.New("invalid gene list name")
	}
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.GeneList{}).Where("id<>? AND external_org_id=? AND (? <> '' OR created_by=?) AND lower(name)=lower(?)", id, g.ExternalOrgID, g.ExternalOrgID, g.CreatedBy, g.Name).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("gene list name already exists")
		}
		result := tx.Model(&model.GeneList{}).Where("id=? AND revision=?", id, req.ExpectedRevision).Updates(map[string]interface{}{"name": g.Name, "description": g.Description, "disease_category": g.DiseaseCategory, "genes_json": g.GenesJSON, "category": g.Category, "revision": gorm.Expr("revision+1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrResourceConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	g.Revision++
	r := geneListResponse(g, a)
	return &r, nil
}
func (s *GeneListService) Delete(id string, actors ...model.OverlayActor) error {
	a := model.OverlayActor{}
	if len(actors) > 0 {
		a = actors[0]
	}
	g, err := s.repo.FindScopedByStringID(id, a)
	if err != nil {
		return errors.New("gene list not found")
	}
	if !a.ResourceMaintenance(g.CreatedBy) {
		return ErrResourceForbidden
	}
	return s.repo.DeleteByID(id)
}
func (s *GeneListService) Publish(id string, a model.OverlayActor, expected uint64) (*model.GeneListResponse, error) {
	g, err := s.repo.FindScopedByStringID(id, a)
	if err != nil {
		return nil, errors.New("gene list not found")
	}
	if a.OrgID == "" || g.ExternalOrgID != "" || !a.ResourceMaintenance(g.CreatedBy) {
		return nil, ErrResourceForbidden
	}
	res := database.GetDB().Model(&model.GeneList{}).Where("id=? AND external_org_id='' AND revision=?", id, expected).Updates(map[string]interface{}{"external_org_id": a.OrgID, "revision": gorm.Expr("revision+1")})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, ErrResourceConflict
	}
	g.ExternalOrgID = a.OrgID
	g.Revision++
	r := geneListResponse(g, a)
	return &r, nil
}

func (s *GeneListService) GetScoped(id string, a model.OverlayActor) (*model.GeneList, error) {
	return s.repo.FindScopedByStringID(id, a)
}
