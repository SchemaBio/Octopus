package middleware

import (
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/gin-gonic/gin"
)

func CORS(cfg *config.ServerConfig) gin.HandlerFunc {
	allowedOrigins := strings.Split(cfg.AllowedOrigins, ",")

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// Check if origin is allowed
		allowOrigin := ""
		for _, o := range allowedOrigins {
			if strings.TrimSpace(o) == origin {
				allowOrigin = origin
				break
			}
		}

		// In debug mode, also allow requests without Origin header (same-origin / server-side)
		if allowOrigin == "" && cfg.Mode == "debug" && origin == "" {
			allowOrigin = "*"
		}

		if allowOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Expose-Headers", "Set-Cookie")
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie, X-CSRF-Token")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
