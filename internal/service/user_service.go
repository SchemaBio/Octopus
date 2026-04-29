package service

import (
	"errors"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
)

// UserService handles user business logic
type UserService struct {
	cfg    *config.Config
	repo   *repository.UserRepository
	jwtSvc *JWTService
}

// NewUserService creates a new user service
func NewUserService(cfg *config.Config) *UserService {
	return &UserService{
		cfg:    cfg,
		repo:   repository.NewUserRepository(),
		jwtSvc: NewJWTService(cfg),
	}
}

// Login authenticates user and returns tokens
func (s *UserService) Login(username, password string) (*model.LoginResponse, error) {
	// Find user
	user, err := s.repo.FindByUsername(username)
	if err != nil {
		return nil, errors.New("invalid username or password")
	}

	// Check password
	if !CheckPassword(password, user.Password) {
		return nil, errors.New("invalid username or password")
	}

	// Check if user is active
	if !user.Active {
		return nil, errors.New("user account is disabled")
	}

	// Generate tokens
	token, refreshToken, expiresAt, err := s.jwtSvc.GenerateToken(user)
	if err != nil {
		return nil, errors.New("failed to generate token")
	}

	return &model.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		User:         *user,
	}, nil
}

// Register creates a new user account
func (s *UserService) Register(req *model.RegisterRequest) (*model.LoginResponse, error) {
	// Check if username exists
	if s.repo.ExistsByUsername(req.Username) {
		return nil, errors.New("username already exists")
	}

	// Check if email exists (if provided)
	if req.Email != "" && s.repo.ExistsByEmail(req.Email) {
		return nil, errors.New("email already exists")
	}

	// Hash password
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		return nil, errors.New("failed to hash password")
	}

	// Create user
	user := &model.User{
		Username: req.Username,
		Password: hashedPassword,
		Email:    req.Email,
		Role:     model.RoleUser,
		Active:   true,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, errors.New("failed to create user")
	}

	// Generate tokens
	token, refreshToken, expiresAt, err := s.jwtSvc.GenerateToken(user)
	if err != nil {
		return nil, errors.New("failed to generate token")
	}

	return &model.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		User:         *user,
	}, nil
}

// RefreshToken refreshes access token
func (s *UserService) RefreshToken(refreshToken string) (*model.RefreshResponse, error) {
	token, newRefreshToken, expiresAt, err := s.jwtSvc.RefreshToken(refreshToken)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	return &model.RefreshResponse{
		Token:        token,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// GetUserByID gets user by ID
func (s *UserService) GetUserByID(id uint) (*model.User, error) {
	return s.repo.FindByID(id)
}

// GetUserByUsername gets user by username
func (s *UserService) GetUserByUsername(username string) (*model.User, error) {
	return s.repo.FindByUsername(username)
}

// CreateDefaultAdmin creates default admin user if not exists
func (s *UserService) CreateDefaultAdmin(username, password string) (*model.User, error) {
	hashedPassword, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateDefaultAdmin(username, hashedPassword)
}

// ListUsers lists all users
func (s *UserService) ListUsers() ([]model.User, error) {
	return s.repo.FindAll()
}

// UpdateUser updates user information
func (s *UserService) UpdateUser(id uint, email string, role string, active bool) error {
	user, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	if email != "" {
		user.Email = email
	}
	if role != "" {
		user.Role = role
	}
	user.Active = active

	return s.repo.Update(user)
}

// ChangePassword changes user password
func (s *UserService) ChangePassword(id uint, oldPassword, newPassword string) error {
	user, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	// Verify old password
	if !CheckPassword(oldPassword, user.Password) {
		return errors.New("invalid old password")
	}

	// Hash new password
	hashedPassword, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	return s.repo.UpdatePassword(id, hashedPassword)
}