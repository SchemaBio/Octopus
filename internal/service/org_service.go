package service

import (
	"errors"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
)

// OrganizationService handles organization business logic
type OrganizationService struct {
	cfg     *config.Config
	orgRepo *repository.OrganizationRepository
	uoRepo  *repository.UserOrganizationRepository
}

// NewOrganizationService creates a new organization service
func NewOrganizationService(cfg *config.Config) *OrganizationService {
	return &OrganizationService{
		cfg:     cfg,
		orgRepo: repository.NewOrganizationRepository(),
		uoRepo:  repository.NewUserOrganizationRepository(),
	}
}

// GetUserOrganizations returns all organizations a user belongs to
func (s *OrganizationService) GetUserOrganizations(userID uint) ([]model.OrganizationWithRole, error) {
	memberships, err := s.uoRepo.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	var result []model.OrganizationWithRole
	for _, m := range memberships {
		org, err := s.orgRepo.FindByStringID(m.OrgID)
		if err != nil {
			continue
		}
		result = append(result, model.OrganizationWithRole{
			ID:          org.ID,
			Name:        org.Name,
			Slug:        org.Slug,
			Description: org.Description,
			OrgRole:     m.Role,
			JoinedAt:    m.JoinedAt.Format(time.RFC3339),
		})
	}

	return result, nil
}

// GetOrganization returns a single organization by ID
func (s *OrganizationService) GetOrganization(id string) (*model.Organization, error) {
	return s.orgRepo.FindByStringID(id)
}

// CreateOrganization creates a new organization and adds the creator as owner
func (s *OrganizationService) CreateOrganization(name, slug, description string, creatorID uint) (*model.Organization, error) {
	org := &model.Organization{
		ID:          uuid.New().String(),
		Name:        name,
		Slug:        slug,
		Description: description,
	}

	if err := s.orgRepo.Create(org); err != nil {
		return nil, err
	}

	// Add creator as owner
	membership := &model.UserOrganization{
		UserID: creatorID,
		OrgID:  org.ID,
		Role:   "owner",
	}
	if err := s.uoRepo.Create(membership); err != nil {
		return nil, err
	}

	return org, nil
}

// SwitchOrganization validates and returns the target organization
func (s *OrganizationService) SwitchOrganization(userID uint, orgID string) (*model.OrganizationWithRole, error) {
	// Check if user is a member of the target org
	membership, err := s.uoRepo.FindByUserAndOrg(userID, orgID)
	if err != nil {
		return nil, errors.New("you are not a member of this organization")
	}

	org, err := s.orgRepo.FindByStringID(orgID)
	if err != nil {
		return nil, errors.New("organization not found")
	}

	return &model.OrganizationWithRole{
		ID:          org.ID,
		Name:        org.Name,
		Slug:        org.Slug,
		Description: org.Description,
		OrgRole:     membership.Role,
		JoinedAt:    membership.JoinedAt.Format(time.RFC3339),
	}, nil
}
