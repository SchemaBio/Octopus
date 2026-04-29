package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
)

// OrganizationRepository provides organization-specific operations
type OrganizationRepository struct {
	*Repository[model.Organization]
}

// NewOrganizationRepository creates a new organization repository
func NewOrganizationRepository() *OrganizationRepository {
	return &OrganizationRepository{
		Repository: NewRepository[model.Organization](),
	}
}

// FindBySlug finds an organization by slug
func (r *OrganizationRepository) FindBySlug(slug string) (*model.Organization, error) {
	return r.FindOneByCondition(map[string]interface{}{"slug": slug})
}

// UserOrganizationRepository provides user-org relationship operations
type UserOrganizationRepository struct {
	*Repository[model.UserOrganization]
}

// NewUserOrganizationRepository creates a new user-org repository
func NewUserOrganizationRepository() *UserOrganizationRepository {
	return &UserOrganizationRepository{
		Repository: NewRepository[model.UserOrganization](),
	}
}

// FindByUserID finds all org memberships for a user
func (r *UserOrganizationRepository) FindByUserID(userID uint) ([]model.UserOrganization, error) {
	return r.FindByCondition(map[string]interface{}{"user_id": userID})
}

// FindByUserAndOrg finds a specific user-org membership
func (r *UserOrganizationRepository) FindByUserAndOrg(userID uint, orgID string) (*model.UserOrganization, error) {
	return r.FindOneByCondition(map[string]interface{}{"user_id": userID, "org_id": orgID})
}

// ExistsByUserAndOrg checks if user is a member of org
func (r *UserOrganizationRepository) ExistsByUserAndOrg(userID uint, orgID string) bool {
	var count int64
	r.db.Model(&model.UserOrganization{}).
		Where("user_id = ? AND org_id = ?", userID, orgID).
		Count(&count)
	return count > 0
}
