package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

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

func tokenClaimsMatchUser(claims *Claims, user *model.User) bool {
	if claims.TokenVersion <= 0 {
		return false
	}
	return claims.UserID == user.ID &&
		claims.Email == user.Email &&
		claims.Role == string(user.SystemRole) &&
		claims.TokenVersion == EffectiveTokenVersion(user.TokenVersion)
}

func resetTokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}

func newResetToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
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
	if err := ValidatePasswordStrength(req.Password); err != nil {
		return nil, err
	}

	// Hash password (with optional client-side hash)
	preparedPassword := PreparePassword(req.Password, req.Email, s.cfg.JWT.ClientPasswordHashEnabled)
	hashedPassword, err := HashPassword(preparedPassword)
	if err != nil {
		return nil, errors.New("failed to hash password")
	}

	// Create user with email as username (internal)
	user := &model.User{
		Username:     req.Email,
		Password:     hashedPassword,
		Email:        req.Email,
		Name:         req.Name,
		SystemRole:   model.SystemRoleUser,
		IsActive:     true,
		TokenVersion: 1,
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
	claims, err := s.jwtSvc.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	user, err := s.repo.FindByID(claims.UserID)
	if err != nil || !user.IsActive || !tokenClaimsMatchUser(claims, user) {
		return nil, errors.New("invalid refresh token")
	}

	token, newRefreshToken, expiresAt, err := s.jwtSvc.GenerateToken(user)
	if err != nil {
		return nil, errors.New("failed to generate token")
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
		user, err := s.repo.FindByEmail(email)
		if err != nil {
			return nil, err
		}
		if user.SystemRole != model.SystemRoleSuperAdmin || !user.IsActive {
			return nil, errors.New("DEFAULT_ADMIN_EMAIL already belongs to a non-active or non-admin account")
		}
		return user, nil
	}

	preparedPassword := PreparePassword(password, email, s.cfg.JWT.ClientPasswordHashEnabled)
	hashedPassword, err := HashPassword(preparedPassword)
	if err != nil {
		return nil, err
	}

	admin := &model.User{
		Username:     email,
		Password:     hashedPassword,
		Email:        email,
		Name:         name,
		SystemRole:   model.SystemRoleSuperAdmin,
		IsActive:     true,
		TokenVersion: 1,
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
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
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
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return err
	}

	preparedNew := PreparePassword(newPassword, user.Email, s.cfg.JWT.ClientPasswordHashEnabled)
	hashedPassword, err := HashPassword(preparedNew)
	if err != nil {
		return err
	}

	return s.repo.UpdatePassword(id, hashedPassword)
}

// RevokeUserTokens invalidates all existing access and refresh tokens for a user.
func (s *UserService) RevokeUserTokens(id uint) error {
	return s.repo.IncrementTokenVersion(id)
}

// RevokeToken invalidates the session represented by a current access or refresh token.
func (s *UserService) RevokeToken(token string) error {
	claims, err := s.jwtSvc.ValidateToken(token)
	if err != nil {
		return err
	}
	user, err := s.repo.FindByID(claims.UserID)
	if err != nil {
		return err
	}
	if !tokenClaimsMatchUser(claims, user) {
		return errors.New("token is no longer current")
	}
	return s.repo.IncrementTokenVersion(user.ID)
}

// GenerateResetToken creates a password reset token for a user.
func (s *UserService) GenerateResetToken(email string) (string, error) {
	user, err := s.repo.FindByEmail(email)
	if err != nil {
		return "", nil
	}

	token, err := newResetToken()
	if err != nil {
		return "", err
	}
	expiry := time.Now().Add(1 * time.Hour)
	user.ResetToken = resetTokenDigest(token)
	user.ResetTokenExpiry = &expiry

	if err := s.repo.Update(user); err != nil {
		return "", err
	}

	return token, nil
}

// ResetPassword resets password using a reset token.
func (s *UserService) ResetPassword(token, newPassword string) error {
	user, err := s.repo.FindByResetToken(resetTokenDigest(token))
	if err != nil {
		return errors.New("invalid or expired reset token")
	}

	if user.ResetTokenExpiry == nil || time.Now().After(*user.ResetTokenExpiry) {
		return errors.New("reset token has expired")
	}
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return err
	}

	preparedPassword := PreparePassword(newPassword, user.Email, s.cfg.JWT.ClientPasswordHashEnabled)
	hashedPassword, err := HashPassword(preparedPassword)
	if err != nil {
		return err
	}

	user.Password = hashedPassword
	user.ResetToken = ""
	user.ResetTokenExpiry = nil
	user.TokenVersion = EffectiveTokenVersion(user.TokenVersion) + 1

	return s.repo.Update(user)
}
