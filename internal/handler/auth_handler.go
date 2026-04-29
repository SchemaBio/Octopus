package handler

import (
	"net/http"
	"strconv"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	cfg         *config.Config
	userService *service.UserService
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		cfg:         cfg,
		userService: service.NewUserService(cfg),
	}
}

// Login handles user login with email + password
func (h *AuthHandler) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	resp, err := h.userService.Login(req.Email, req.Password)
	if err != nil {
		ErrorUnauthorized(c, err.Error())
		return
	}

	Success(c, resp)
}

// Register handles user registration
func (h *AuthHandler) Register(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	resp, err := h.userService.Register(&req)
	if err != nil {
		if err.Error() == "email already exists" {
			ErrorConflict(c, err.Error())
		} else {
			ErrorInternal(c, err.Error())
		}
		return
	}

	SuccessCreated(c, resp)
}

// Refresh handles token refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req model.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	resp, err := h.userService.RefreshToken(req.RefreshToken)
	if err != nil {
		ErrorUnauthorized(c, err.Error())
		return
	}

	Success(c, resp)
}

// Me returns the current authenticated user
func (h *AuthHandler) Me(c *gin.Context) {
	userID, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}

	user, err := h.userService.GetUserByID(userID)
	if err != nil {
		ErrorNotFound(c, "User not found")
		return
	}

	Success(c, model.UserToResponse(user))
}

// Logout handles user logout (stateless - client discards tokens)
func (h *AuthHandler) Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// --- User management handlers (admin) ---

type UserHandler struct {
	svc *service.UserService
}

func NewUserHandler(cfg *config.Config) *UserHandler {
	return &UserHandler{
		svc: service.NewUserService(cfg),
	}
}

// ListUsers returns paginated user list
func (h *UserHandler) ListUsers(c *gin.Context) {
	var query model.UserListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 10
	}

	resp, err := h.svc.ListUsers(&query)
	if err != nil {
		ErrorInternal(c, err.Error())
		return
	}

	SuccessList(c, resp.Items, resp.Total, query.Page, query.PageSize)
}

// GetUser returns a single user by ID
func (h *UserHandler) GetUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		ErrorBadRequest(c, "invalid user ID")
		return
	}

	user, err := h.svc.GetUserByID(uint(id))
	if err != nil {
		ErrorNotFound(c, "User not found")
		return
	}

	Success(c, model.UserToResponse(user))
}

// CreateUser creates a new user (admin only)
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req model.UserCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	// Register via service
	regReq := &model.RegisterRequest{
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
	}
	resp, err := h.svc.Register(regReq)
	if err != nil {
		if err.Error() == "email already exists" {
			ErrorConflict(c, err.Error())
		} else {
			ErrorInternal(c, err.Error())
		}
		return
	}

	// Update system role if specified
	if req.SystemRole != "" {
		user, _ := h.svc.GetUserByEmail(req.Email)
		if user != nil {
			user.SystemRole = req.SystemRole
			if req.PrimaryOrgID != "" {
				user.PrimaryOrgID = req.PrimaryOrgID
			}
			h.svc.UpdateUser(user.ID, &model.UserUpdateRequest{
				SystemRole:   req.SystemRole,
				PrimaryOrgID: req.PrimaryOrgID,
			})
		}
	}

	SuccessCreated(c, resp.User)
}

// UpdateUser updates user information
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		ErrorBadRequest(c, "invalid user ID")
		return
	}

	var req model.UserUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	user, err := h.svc.UpdateUser(uint(id), &req)
	if err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	Success(c, model.UserToResponse(user))
}

// DeleteUser deletes a user
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		ErrorBadRequest(c, "invalid user ID")
		return
	}

	if err := h.svc.DeleteUser(uint(id)); err != nil {
		ErrorNotFound(c, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}
