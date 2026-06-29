package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/gin-gonic/gin"
)

func testExternalAuthConfig() *config.Config {
	return &config.Config{
		JWT: config.JWTConfig{
			Secret:          "test-secret-with-at-least-32-characters",
			Issuer:          "octopus-test",
			ExpireDuration:  time.Hour,
			RefreshDuration: 24 * time.Hour,
		},
		ExternalAuth: config.ExternalAuthConfig{
			Enabled:      true,
			SharedSecret: "shared-overlay-secret",
			HeaderName:   "X-Octopus-External-Auth",
			UserIDHeader: "X-Octopus-User-ID",
			EmailHeader:  "X-Octopus-User-Email",
			RoleHeader:   "X-Octopus-User-Role",
			OrgIDHeader:  "X-Octopus-Org-ID",
		},
	}
}

func TestJWTAuthAcceptsTrustedExternalAuthHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth(testExternalAuthConfig()))
	router.GET("/protected", func(c *gin.Context) {
		userID, email, role, ok := GetCurrentUser(c)
		orgID, hasOrg := GetCurrentOrg(c)
		if !ok || !hasOrg {
			t.Fatalf("expected external user and organization in context")
		}
		if userID != 42 || email != "user@example.com" || role != "org_admin" || orgID != "org-1" {
			t.Fatalf("unexpected context values: userID=%d email=%q role=%q orgID=%q", userID, email, role, orgID)
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Octopus-External-Auth", "Bearer shared-overlay-secret")
	req.Header.Set("X-Octopus-User-ID", "42")
	req.Header.Set("X-Octopus-User-Email", "user@example.com")
	req.Header.Set("X-Octopus-User-Role", "org_admin")
	req.Header.Set("X-Octopus-Org-ID", "org-1")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected trusted external auth to pass, got %d", resp.Code)
	}
}

func TestJWTAuthRejectsInvalidExternalIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth(testExternalAuthConfig()))
	router.GET("/protected", func(c *gin.Context) {
		t.Fatal("handler should not run for invalid external identity")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Octopus-External-Auth", "shared-overlay-secret")
	req.Header.Set("X-Octopus-User-ID", "not-a-number")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid external identity to be unauthorized, got %d", resp.Code)
	}
}

func TestJWTAuthFallsBackWhenExternalSecretDoesNotMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth(testExternalAuthConfig()))
	router.GET("/protected", func(c *gin.Context) {
		t.Fatal("handler should not run without a valid JWT")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Octopus-External-Auth", "wrong-secret")
	req.Header.Set("X-Octopus-User-ID", "42")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected fallback JWT auth to reject missing token, got %d", resp.Code)
	}
}
