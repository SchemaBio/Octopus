package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
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
	return r.FindByCondition(map[string]interface{}{"active": true})
}

// FindByRole finds users by role
func (r *UserRepository) FindByRole(role string) ([]model.User, error) {
	return r.FindByCondition(map[string]interface{}{"role": role})
}

// UpdatePassword updates user password
func (r *UserRepository) UpdatePassword(userID uint, hashedPassword string) error {
	return r.db.Model(&model.User{}).Where("id = ?", userID).Update("password", hashedPassword).Error
}

// UpdateActive updates user active status
func (r *UserRepository) UpdateActive(userID uint, active bool) error {
	return r.db.Model(&model.User{}).Where("id = ?", userID).Update("active", active).Error
}

// CreateDefaultAdmin creates default admin user if not exists
func (r *UserRepository) CreateDefaultAdmin(username, hashedPassword string) (*model.User, error) {
	// Check if admin exists
	if r.ExistsByUsername(username) {
		return r.FindByUsername(username)
	}

	// Create admin
	admin := &model.User{
		Username: username,
		Password: hashedPassword,
		Role:     model.RoleAdmin,
		Active:   true,
	}

	err := r.Create(admin)
	if err != nil {
		return nil, err
	}

	return admin, nil
}