package service

import (
	"errors"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
)

// PedigreeService handles pedigree business logic
type PedigreeService struct {
	cfg        *config.Config
	repo       *repository.PedigreeRepository
	memberRepo *repository.PedigreeMemberRepository
}

// NewPedigreeService creates a new pedigree service
func NewPedigreeService(cfg *config.Config) *PedigreeService {
	return &PedigreeService{
		cfg:        cfg,
		repo:       repository.NewPedigreeRepository(),
		memberRepo: repository.NewPedigreeMemberRepository(),
	}
}

// List returns paginated pedigrees
func (s *PedigreeService) List(query *model.PedigreeListQuery) (*model.PedigreeListResponse, error) {
	pedigrees, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.PedigreeResponse, len(pedigrees))
	for i, p := range pedigrees {
		items[i] = model.PedigreeResponse{
			ID:          p.ID,
			Name:        p.Name,
			Disease:     p.Disease,
			MemberCount: s.repo.CountMembers(p.ID),
			CreatedAt:   p.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   p.UpdatedAt.Format(time.RFC3339),
		}
	}

	return &model.PedigreeListResponse{
		Total: int(total),
		Items: items,
	}, nil
}

// Get returns a pedigree with all members
func (s *PedigreeService) Get(id string) (*model.PedigreeDetailResponse, error) {
	pedigree, members, err := s.repo.FindByIDWithMembers(id)
	if err != nil {
		return nil, err
	}

	memberResps := make([]model.PedigreeMemberResp, len(members))
	for i, m := range members {
		memberResps[i] = MemberToResponse(&m)
	}

	return &model.PedigreeDetailResponse{
		ID:              pedigree.ID,
		Name:            pedigree.Name,
		Disease:         pedigree.Disease,
		Note:            pedigree.Note,
		ProbandMemberID: pedigree.ProbandMemberID,
		Members:         memberResps,
		CreatedAt:       pedigree.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       pedigree.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// Create creates a new pedigree
func (s *PedigreeService) Create(req *model.PedigreeCreateRequest, userID uint) (*model.Pedigree, error) {
	pedigree := &model.Pedigree{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Disease:   req.Disease,
		Note:      req.Note,
		CreatedBy: userID,
	}

	if err := s.repo.Create(pedigree); err != nil {
		return nil, err
	}

	return pedigree, nil
}

// Update updates a pedigree
func (s *PedigreeService) Update(id string, req *model.PedigreeUpdateRequest) (*model.Pedigree, error) {
	pedigree, err := s.repo.FindByStringID(id)
	if err != nil {
		return nil, errors.New("pedigree not found")
	}

	if req.Name != "" {
		pedigree.Name = req.Name
	}
	if req.Disease != "" {
		pedigree.Disease = req.Disease
	}
	if req.Note != "" {
		pedigree.Note = req.Note
	}

	if err := s.repo.Update(pedigree); err != nil {
		return nil, err
	}

	return pedigree, nil
}

// Delete deletes a pedigree and all its members
func (s *PedigreeService) Delete(id string) error {
	// Delete members first
	if err := s.memberRepo.DeleteByPedigreeID(id); err != nil {
		return err
	}
	return s.repo.DeleteByID(id)
}

// SetProband sets a member as the proband
func (s *PedigreeService) SetProband(pedigreeID, memberID string) (*model.PedigreeDetailResponse, error) {
	// Verify pedigree exists
	_, err := s.repo.FindByStringID(pedigreeID)
	if err != nil {
		return nil, errors.New("pedigree not found")
	}

	// Verify member exists
	_, err = s.memberRepo.FindByStringID(memberID)
	if err != nil {
		return nil, errors.New("member not found")
	}

	// Update proband in transaction
	tx := database.GetDB().Begin()
	if err := s.memberRepo.UpdateProband(pedigreeID, memberID, tx); err != nil {
		tx.Rollback()
		return nil, err
	}

	// Update pedigree's proband_member_id
	if err := tx.Model(&model.Pedigree{}).Where("id = ?", pedigreeID).
		Update("proband_member_id", memberID).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	tx.Commit()

	return s.Get(pedigreeID)
}

// --- Member operations ---

// ListMembers returns all members of a pedigree
func (s *PedigreeService) ListMembers(pedigreeID string) ([]model.PedigreeMemberResp, error) {
	members, err := s.memberRepo.FindByPedigreeID(pedigreeID)
	if err != nil {
		return nil, err
	}

	result := make([]model.PedigreeMemberResp, len(members))
	for i, m := range members {
		result[i] = MemberToResponse(&m)
	}

	return result, nil
}

// GetMember returns a single member
func (s *PedigreeService) GetMember(pedigreeID, memberID string) (*model.PedigreeMemberResp, error) {
	member, err := s.memberRepo.FindByStringID(memberID)
	if err != nil {
		return nil, errors.New("member not found")
	}
	if member.PedigreeID != pedigreeID {
		return nil, errors.New("member does not belong to this pedigree")
	}

	resp := MemberToResponse(member)
	return &resp, nil
}

// CreateMember creates a new member in a pedigree
func (s *PedigreeService) CreateMember(pedigreeID string, req *model.PedigreeMemberCreateRequest) (*model.PedigreeMemberResp, error) {
	// Verify pedigree exists
	_, err := s.repo.FindByStringID(pedigreeID)
	if err != nil {
		return nil, errors.New("pedigree not found")
	}

	hasSample := req.SampleID != ""
	member := &model.PedigreeMember{
		ID:             uuid.New().String(),
		PedigreeID:     pedigreeID,
		SampleID:       req.SampleID,
		Name:           req.Name,
		Gender:         req.Gender,
		BirthYear:      req.BirthYear,
		IsDeceased:     req.IsDeceased,
		DeceasedYear:   req.DeceasedYear,
		Relation:       req.Relation,
		AffectedStatus: req.AffectedStatus,
		Phenotypes:     req.Phenotypes,
		FatherID:       req.FatherID,
		MotherID:       req.MotherID,
		Generation:     req.Generation,
		Position:       req.Position,
		HasSample:      hasSample,
	}

	if member.Gender == "" {
		member.Gender = model.GenderUnknown
	}
	if member.AffectedStatus == "" {
		member.AffectedStatus = model.AffectedStatusUnknown
	}

	if err := s.memberRepo.Create(member); err != nil {
		return nil, err
	}

	resp := MemberToResponse(member)
	return &resp, nil
}

// UpdateMember updates a member
func (s *PedigreeService) UpdateMember(pedigreeID, memberID string, req *model.PedigreeMemberUpdateRequest) (*model.PedigreeMemberResp, error) {
	member, err := s.memberRepo.FindByStringID(memberID)
	if err != nil {
		return nil, errors.New("member not found")
	}
	if member.PedigreeID != pedigreeID {
		return nil, errors.New("member does not belong to this pedigree")
	}

	if req.Name != "" {
		member.Name = req.Name
	}
	if req.Gender != "" {
		member.Gender = req.Gender
	}
	if req.BirthYear != nil {
		member.BirthYear = req.BirthYear
	}
	if req.IsDeceased != nil {
		member.IsDeceased = *req.IsDeceased
	}
	if req.DeceasedYear != nil {
		member.DeceasedYear = req.DeceasedYear
	}
	if req.Relation != "" {
		member.Relation = req.Relation
	}
	if req.AffectedStatus != "" {
		member.AffectedStatus = req.AffectedStatus
	}
	if req.Phenotypes != nil {
		member.Phenotypes = req.Phenotypes
	}
	if req.FatherID != "" {
		member.FatherID = req.FatherID
	}
	if req.MotherID != "" {
		member.MotherID = req.MotherID
	}
	if req.Generation != nil {
		member.Generation = *req.Generation
	}
	if req.Position != nil {
		member.Position = *req.Position
	}
	if req.SampleID != "" {
		member.SampleID = req.SampleID
		member.HasSample = true
	}

	if err := s.memberRepo.Update(member); err != nil {
		return nil, err
	}

	resp := MemberToResponse(member)
	return &resp, nil
}

// DeleteMember deletes a member
func (s *PedigreeService) DeleteMember(pedigreeID, memberID string) error {
	member, err := s.memberRepo.FindByStringID(memberID)
	if err != nil {
		return errors.New("member not found")
	}
	if member.PedigreeID != pedigreeID {
		return errors.New("member does not belong to this pedigree")
	}
	return s.memberRepo.DeleteByID(memberID)
}

// MemberToResponse converts a PedigreeMember to PedigreeMemberResp
func MemberToResponse(m *model.PedigreeMember) model.PedigreeMemberResp {
	return model.PedigreeMemberResp{
		ID:             m.ID,
		PedigreeID:     m.PedigreeID,
		SampleID:       m.SampleID,
		Name:           m.Name,
		Gender:         m.Gender,
		BirthYear:      m.BirthYear,
		IsDeceased:     m.IsDeceased,
		DeceasedYear:   m.DeceasedYear,
		Relation:       m.Relation,
		AffectedStatus: m.AffectedStatus,
		Phenotypes:     m.Phenotypes,
		FatherID:       m.FatherID,
		MotherID:       m.MotherID,
		Generation:     m.Generation,
		Position:       m.Position,
		HasSample:      m.HasSample,
		CreatedAt:      m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      m.UpdatedAt.Format(time.RFC3339),
	}
}
