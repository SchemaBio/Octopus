package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCSRFRequiresTokenForRefreshCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CSRF())
	router.POST("/api/v1/auth/refresh", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "refresh-token"})
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected refresh-cookie request without CSRF token to be forbidden, got %d", resp.Code)
	}
}

func TestCSRFAuthLoginDoesNotRequireToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CSRF())
	router.POST("/api/v1/auth/login", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected login without CSRF cookie to pass, got %d", resp.Code)
	}
}

func TestCSRFForgotPasswordDoesNotRequireToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CSRF())
	router.POST("/api/v1/auth/forgot-password", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected forgot-password without CSRF cookie to pass, got %d", resp.Code)
	}
}

func TestCSRFAllowsMatchingHeaderAndCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CSRF())
	router.POST("/api/v1/tasks", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: "access-token"})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "csrf-token"})
	req.Header.Set("X-CSRF-Token", "csrf-token")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected matching CSRF token to pass, got %d", resp.Code)
	}
}
