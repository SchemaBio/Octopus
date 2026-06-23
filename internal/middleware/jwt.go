package middleware

import (
	"net/http"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

// JWTAuth middleware validates JWT token (from header or cookie)
func JWTAuth(cfg *config.Config) gin.HandlerFunc {
	jwtService := service.NewJWTService(cfg)
	userRepo := repository.NewUserRepository()

	return func(c *gin.Context) {
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
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthorized",
			})
			c.Abort()
			return
		}

		if role.(string) != string(model.SystemRoleSuperAdmin) {
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

	return userID.(uint), email.(string), role.(string), true
}
