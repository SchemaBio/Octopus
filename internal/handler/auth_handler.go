package handler

import (
	"errors"
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	cfg        *config.Config
	jwtService *service.JWTService
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		cfg:        cfg,
		jwtService: service.NewJWTService(cfg),
	}
}

// Login godoc
// @Summary User login
// @Description Login with username and password
// @Tags auth
// @Accept json
// @Produce json
// @Param request body model.LoginRequest true "Login credentials"
// @Success 200 {object} model.LoginResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find user (in real implementation, query database)
	// For demo, we use a mock user
	user, err := h.findUserByUsername(req.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	// Check password
	if !service.CheckPassword(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	// Check if user is active
	if !user.Active {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User account is disabled"})
		return
	}

	// Generate tokens
	token, refreshToken, expiresAt, err := h.jwtService.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, model.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		User:         *user,
	})
}

// Register godoc
// @Summary User registration
// @Description Register a new user account
// @Tags auth
// @Accept json
// @Produce json
// @Param request body model.RegisterRequest true "Registration data"
// @Success 201 {object} model.LoginResponse
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/v1/auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if username exists
	if h.usernameExists(req.Username) {
		c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
		return
	}

	// Hash password
	hashedPassword, err := service.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Create user
	user := &model.User{
		Username: req.Username,
		Password: hashedPassword,
		Email:    req.Email,
		Role:     model.RoleUser,
		Active:   true,
	}

	// Save user (in real implementation, save to database)
	if err := h.saveUser(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Generate tokens
	token, refreshToken, expiresAt, err := h.jwtService.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, model.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		User:         *user,
	})
}

// Refresh godoc
// @Summary Refresh token
// @Description Refresh access token using refresh token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body model.RefreshRequest true "Refresh token"
// @Success 200 {object} model.RefreshResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req model.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, refreshToken, expiresAt, err := h.jwtService.RefreshToken(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid refresh token"})
		return
	}

	c.JSON(http.StatusOK, model.RefreshResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	})
}

// Me godoc
// @Summary Get current user
// @Description Get current authenticated user info
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/me [get]
func (h *AuthHandler) Me(c *gin.Context) {
	userID, username, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":  userID,
		"username": username,
		"role":     role,
	})
}

// Logout godoc
// @Summary Logout
// @Description Logout (client should discard tokens)
// @Tags auth
// @Success 200 {object} map[string]string
// @Router /api/v1/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	// JWT is stateless, so logout is handled client-side
	// Client should discard the tokens
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// Mock database functions (replace with real database implementation)
func (h *AuthHandler) findUserByUsername(username string) (*model.User, error) {
	// TODO: Replace with actual database query
	// For demo, create a mock user
	if username == "admin" {
		hashedPassword, _ := service.HashPassword("admin123")
		return &model.User{
			ID:       1,
			Username: "admin",
			Password: hashedPassword,
			Role:     model.RoleAdmin,
			Active:   true,
		}, nil
	}
	if username == "user" {
		hashedPassword, _ := service.HashPassword("user123")
		return &model.User{
			ID:       2,
			Username: "user",
			Password: hashedPassword,
			Role:     model.RoleUser,
			Active:   true,
		}, nil
	}
	return nil, errors.New("user not found")
}

func (h *AuthHandler) usernameExists(username string) bool {
	// TODO: Replace with actual database query
	return username == "admin" || username == "user"
}

func (h *AuthHandler) saveUser(user *model.User) error {
	// TODO: Replace with actual database save
	// Mock: assign an ID
	user.ID = 3
	return nil
}