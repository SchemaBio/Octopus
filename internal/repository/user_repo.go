package repository

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

// UserRepository provides user-specific operations
type UserRepository struct {
	*Repository[model.User]
}

// NewUserRepository creates a new user repository
func NewUserRepository() *UserRepository {
	return &UserRepository{
		Repository: NewRepository[model.User](),
	}
}

// FindByUsername finds a user by username
func (r *UserRepository) FindByUsername(username string) (*model.User, error) {
	return r.FindOneByCondition(map[string]interface{}{"username": username})
}

// FindByEmail finds a user by email
func (r *UserRepository) FindByEmail(email string) (*model.User, error) {
	return r.FindOneByCondition(map[string]interface{}{"email": email})
}

// FindByResetToken finds a user by password reset token
func (r *UserRepository) FindByResetToken(token string) (*model.User, error) {
	return r.FindOneByCondition(map[string]interface{}{"reset_token": token})
}

// ExistsByUsername checks if username exists
func (r *UserRepository) ExistsByUsername(username string) bool {
	var count int64
	r.db.Model(&model.User{}).Where("username = ?", username).Count(&count)
	return count > 0
}

// ExistsByEmail checks if email exists
func (r *UserRepository) ExistsByEmail(email string) bool {
	if email == "" {
		return false
	}
	var count int64
	r.db.Model(&model.User{}).Where("email = ?", email).Count(&count)
	return count > 0
}

// FindActiveUsers finds all active users
func (r *UserRepository) FindActiveUsers() ([]model.User, error) {
	return r.FindByCondition(map[string]interface{}{"is_active": true})
}

// FindByRole finds users by role
func (r *UserRepository) FindByRole(role string) ([]model.User, error) {
	return r.FindByCondition(map[string]interface{}{"system_role": role})
}

// PaginateByQuery finds users with pagination and search
func (r *UserRepository) PaginateByQuery(query *model.UserListQuery) ([]model.User, int64, error) {
	db := r.db.Model(&model.User{})

	if query.Search != "" {
		search := "%" + query.Search + "%"
		db = db.Where("email LIKE ? OR name LIKE ?", search, search)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	var users []model.User
	offset := (page - 1) * pageSize
	err := db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&users).Error
	return users, total, err
}

// UpdatePassword updates user password and revokes existing tokens.
func (r *UserRepository) UpdatePassword(userID uint, hashedPassword string) error {
	return r.db.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"password":      hashedPassword,
		"token_version": nextTokenVersionExpr(),
	}).Error
}

// IncrementTokenVersion revokes all previously issued tokens for a user.
func (r *UserRepository) IncrementTokenVersion(userID uint) error {
	return r.db.Model(&model.User{}).Where("id = ?", userID).UpdateColumn("token_version", nextTokenVersionExpr()).Error
}

func nextTokenVersionExpr() interface{} {
	return gorm.Expr("CASE WHEN token_version IS NULL OR token_version < ? THEN ? ELSE token_version + ? END", 1, 2, 1)
}

// UpdateActive updates user active status
func (r *UserRepository) UpdateActive(userID uint, active bool) error {
	return r.db.Model(&model.User{}).Where("id = ?", userID).Update("is_active", active).Error
}

// CreateDefaultAdmin creates default admin user if not exists
func (r *UserRepository) CreateDefaultAdmin(email, hashedPassword, name string) (*model.User, error) {
	// Check if admin exists by email
	if r.ExistsByEmail(email) {
		return r.FindByEmail(email)
	}

	// Create admin
	admin := &model.User{
		Username:     email,
		Password:     hashedPassword,
		Email:        email,
		Name:         name,
		SystemRole:   model.SystemRoleSuperAdmin,
		IsActive:     true,
		TokenVersion: 1,
	}

	err := r.Create(admin)
	if err != nil {
		return nil, err
	}

	return admin, nil
}
