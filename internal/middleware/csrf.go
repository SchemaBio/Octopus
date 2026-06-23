package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isSafeMethod(c.Request.Method) || isAuthEndpoint(c.Request.URL.Path) {
			c.Next()
			return
		}

		if _, err := c.Cookie("access_token"); err != nil {
			if _, refreshErr := c.Cookie("refresh_token"); refreshErr != nil {
				c.Next()
				return
			}
		}

		cookieToken, err := c.Cookie("csrf_token")
		if err != nil || cookieToken == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token is required"})
			c.Abort()
			return
		}

		headerToken := c.GetHeader("X-CSRF-Token")
		if headerToken == "" || subtle.ConstantTimeCompare([]byte(headerToken), []byte(cookieToken)) != 1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Invalid CSRF token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func isAuthEndpoint(path string) bool {
	switch strings.TrimPrefix(path, "/api/v1/auth/") {
	case "login", "register":
		return true
	default:
		return false
	}
}
