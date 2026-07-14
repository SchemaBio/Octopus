package middleware

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

// mapExternalRole translates trusted-overlay role names (forwarded by Squid,
// e.g. PLATFORM_ADMIN / ORG_USER) into Octopus SystemRole values.
//
// The overlay is trusted on identity (it proved the shared secret), but role
// strings are NOT a security boundary — the secret is. Unknown or empty
// roles therefore collapse to USER (least privilege / fail-closed) rather
// than escalating: we never map an unrecognized string to SUPER_ADMIN. An
// already-Octopus-shaped role ("SUPER_ADMIN"/"USER") passes through unchanged.
func mapExternalRole(role string) string {
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "PLATFORM_ADMIN", "PLATFORMADMIN":
		return string(model.SystemRoleSuperAdmin)
	case "ORG_USER", "ORGUSER", "USER":
		return string(model.SystemRoleUser)
	default:
		if role == string(model.SystemRoleSuperAdmin) || role == string(model.SystemRoleUser) {
			return role
		}
		return string(model.SystemRoleUser)
	}
}

func applyExternalAuth(c *gin.Context, cfg *config.Config) bool {
	if cfg == nil || !cfg.ExternalAuth.Enabled || cfg.ExternalAuth.SharedSecret == "" {
		return false
	}

	token := bearerToken(c.GetHeader(cfg.ExternalAuth.HeaderName))
	if token == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.ExternalAuth.SharedSecret)) != 1 {
		return false
	}

	userID, err := strconv.ParseUint(strings.TrimSpace(c.GetHeader(cfg.ExternalAuth.UserIDHeader)), 10, 64)
	if err != nil || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid external user identity"})
		c.Abort()
		return true
	}

	email := strings.TrimSpace(c.GetHeader(cfg.ExternalAuth.EmailHeader))
	role := mapExternalRole(c.GetHeader(cfg.ExternalAuth.RoleHeader))
	orgID := strings.TrimSpace(c.GetHeader(cfg.ExternalAuth.OrgIDHeader))

	c.Set("user_id", uint(userID))
	c.Set("email", email)
	c.Set("role", role)
	if orgID != "" {
		c.Set("org_id", orgID)
	}
	c.Set("external_auth", true)
	return true
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return header
}

// JWTAuth middleware validates JWT token (from header or cookie)
func JWTAuth(cfg *config.Config) gin.HandlerFunc {
	jwtService := service.NewJWTService(cfg)
	userRepo := repository.NewUserRepository()

	return func(c *gin.Context) {
		if applyExternalAuth(c, cfg) {
			c.Next()
			return
		}

		tokenString := ""

		// Try Authorization header first
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString = parts[1]
			}
		}

		// Fall back to cookie
		if tokenString == "" {
			cookie, err := c.Cookie("access_token")
			if err == nil && cookie != "" {
				tokenString = cookie
			}
		}

		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization token is required",
			})
			c.Abort()
			return
		}

		// Validate token
		claims, err := jwtService.ValidateAccessToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid or expired token",
			})
			c.Abort()
			return
		}

		// Verify user still exists and is active (skip if DB unavailable)
		if database.GetDB() != nil {
			user, err := userRepo.FindByID(claims.UserID)
			if err != nil || !claimsMatchUser(claims, user) {
				c.JSON(http.StatusUnauthorized, gin.H{
					"error": "Account is not active",
				})
				c.Abort()
				return
			}
		}

		// Set user info in context
		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// OptionalJWTAuth middleware that allows both authenticated and anonymous requests
func OptionalJWTAuth(cfg *config.Config) gin.HandlerFunc {
	jwtService := service.NewJWTService(cfg)
	userRepo := repository.NewUserRepository()

	return func(c *gin.Context) {
		if applyExternalAuth(c, cfg) {
			c.Next()
			return
		}

		tokenString := ""

		// Try Authorization header first
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString = parts[1]
			}
		}

		// Fall back to cookie
		if tokenString == "" {
			cookie, err := c.Cookie("access_token")
			if err == nil && cookie != "" {
				tokenString = cookie
			}
		}

		if tokenString == "" {
			c.Next()
			return
		}

		claims, err := jwtService.ValidateAccessToken(tokenString)
		if err != nil {
			c.Next()
			return
		}

		// Verify user still exists and is active (skip if DB unavailable)
		if database.GetDB() != nil {
			user, err := userRepo.FindByID(claims.UserID)
			if err != nil || !claimsMatchUser(claims, user) {
				c.Next()
				return
			}
		}

		// Set user info in context
		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// claimsMatchUser verifies that JWT claims still match the database user state.
func claimsMatchUser(claims *service.Claims, user *model.User) bool {
	if claims.TokenVersion <= 0 {
		return false
	}
	return user.Email == claims.Email &&
		string(user.SystemRole) == claims.Role &&
		user.IsActive &&
		claims.TokenVersion == service.EffectiveTokenVersion(user.TokenVersion)
}

// RequireAdmin checks if user has admin role
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		roleString, ok := role.(string)
		if !exists || !ok || roleString != string(model.SystemRoleSuperAdmin) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Admin access required",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// GetCurrentUser gets current user info from context
// Returns: userID, email, role, ok
func GetCurrentUser(c *gin.Context) (uint, string, string, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, "", "", false
	}
	email, _ := c.Get("email")
	role, _ := c.Get("role")
	userIDValue, ok := userID.(uint)
	if !ok {
		return 0, "", "", false
	}
	emailValue, ok := email.(string)
	if !ok {
		return 0, "", "", false
	}
	roleValue, ok := role.(string)
	if !ok {
		return 0, "", "", false
	}

	return userIDValue, emailValue, roleValue, true
}

// GetCurrentOrg gets the optional organization ID forwarded by a trusted overlay.
func GetCurrentOrg(c *gin.Context) (string, bool) {
	orgID, exists := c.Get("org_id")
	if !exists {
		return "", false
	}
	value, ok := orgID.(string)
	return value, ok && value != ""
}
