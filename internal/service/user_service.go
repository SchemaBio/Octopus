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

// Login authenticates user by email and returns tokens
func (s *UserService) Login(email, password string) (*model.LoginResponse, error) {
	// Find user by email
	user, err := s.repo.FindByEmail(email)
	if err != nil {
		return nil, errors.New("invalid email or password")
	}

	// Check password (with optional client-side hash)
	preparedPassword := PreparePassword(password, email, s.cfg.JWT.ClientPasswordHashEnabled)
	if !CheckPassword(preparedPassword, user.Password) {
		return nil, errors.New("invalid email or password")
	}

	// Check if user is active
	if !user.IsActive {
		return nil, errors.New("user account is disabled")
	}

	// Generate tokens
	accessToken, refreshToken, expiresAt, err := s.jwtSvc.GenerateToken(user)
	if err != nil {
		return nil, errors.New("failed to generate token")
	}

	// Build response
	userResp := model.UserToResponse(user)

	return &model.LoginResponse{
		User:          userResp,
		Organizations: []model.OrganizationInfo{},
		AccessToken:   accessToken,
		RefreshToken:  refreshToken,
		ExpiresAt:     expiresAt,
	}, nil
}

// Register creates a new user account
func (s *UserService) Register(req *model.RegisterRequest) (*model.LoginResponse, error) {
	// Check if email exists
	if s.repo.ExistsByEmail(req.Email) {
		return nil, errors.New("email already exists")
	}

	// Hash password (with optional client-side hash)
	preparedPassword := PreparePassword(req.Password, req.Email, s.cfg.JWT.ClientPasswordHashEnabled)
	hashedPassword, err := HashPassword(preparedPassword)
	if err != nil {
		return nil, errors.New("failed to hash password")
	}

	// Create user with email as username (internal)
	user := &model.User{
		Username:   req.Email,
		Password:   hashedPassword,
		Email:      req.Email,
		Name:       req.Name,
		SystemRole: model.SystemRoleUser,
		IsActive:   true,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, errors.New("failed to create user")
	}

	// Generate tokens
	accessToken, refreshToken, expiresAt, err := s.jwtSvc.GenerateToken(user)
	if err != nil {
		return nil, errors.New("failed to generate token")
	}

	userResp := model.UserToResponse(user)

	return &model.LoginResponse{
		User:          userResp,
		Organizations: []model.OrganizationInfo{},
		AccessToken:   accessToken,
		RefreshToken:  refreshToken,
		ExpiresAt:     expiresAt,
	}, nil
}

// RefreshToken refreshes access token
func (s *UserService) RefreshToken(refreshToken string) (*model.RefreshResponse, error) {
	token, newRefreshToken, expiresAt, err := s.jwtSvc.RefreshToken(refreshToken)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	return &model.RefreshResponse{
		AccessToken:  token,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// GetUserByID gets user by ID
func (s *UserService) GetUserByID(id uint) (*model.User, error) {
	return s.repo.FindByID(id)
}

// GetUserByEmail gets user by email
func (s *UserService) GetUserByEmail(email string) (*model.User, error) {
	return s.repo.FindByEmail(email)
}

// CreateDefaultAdmin creates default admin user if not exists
func (s *UserService) CreateDefaultAdmin(email, password, name string) (*model.User, error) {
	// Check if admin exists by email
	if s.repo.ExistsByEmail(email) {
		return s.repo.FindByEmail(email)
	}

	preparedPassword := PreparePassword(password, email, s.cfg.JWT.ClientPasswordHashEnabled)
	hashedPassword, err := HashPassword(preparedPassword)
	if err != nil {
		return nil, err
	}

	admin := &model.User{
		Username:   email,
		Password:   hashedPassword,
		Email:      email,
		Name:       name,
		SystemRole: model.SystemRoleSuperAdmin,
		IsActive:   true,
	}

	if err := s.repo.Create(admin); err != nil {
		return nil, err
	}

	return admin, nil
}

// ListUsers lists users with pagination
func (s *UserService) ListUsers(query *model.UserListQuery) (*model.UserListResponse, error) {
	users, total, err := s.repo.PaginateByQuery(query)
	if err != nil {
		return nil, err
	}

	items := make([]model.UserResponse, len(users))
	for i, u := range users {
		items[i] = model.UserToResponse(&u)
	}

	return &model.UserListResponse{
		Total: total,
		Items: items,
	}, nil
}

// UpdateUser updates user information
func (s *UserService) UpdateUser(id uint, req *model.UserUpdateRequest) (*model.User, error) {
	user, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		user.Name = req.Name
	}
	if req.SystemRole != "" {
		user.SystemRole = req.SystemRole
	}

	if err := s.repo.Update(user); err != nil {
		return nil, err
	}

	return user, nil
}

// DeleteUser deletes a user
func (s *UserService) DeleteUser(id uint) error {
	return s.repo.Delete(id)
}

// ChangePassword changes user password
func (s *UserService) ChangePassword(id uint, oldPassword, newPassword string) error {
	user, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	preparedOld := PreparePassword(oldPassword, user.Email, s.cfg.JWT.ClientPasswordHashEnabled)
	if !CheckPassword(preparedOld, user.Password) {
		return errors.New("invalid old password")
	}

	preparedNew := PreparePassword(newPassword, user.Email, s.cfg.JWT.ClientPasswordHashEnabled)
	hashedPassword, err := HashPassword(preparedNew)
	if err != nil {
		return err
	}

	return s.repo.UpdatePassword(id, hashedPassword)
}
