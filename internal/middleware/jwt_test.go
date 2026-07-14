package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
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
		// Squid forwards the platform-admin overlay role; Octopus must map it
		// to its own SUPER_ADMIN (not pass "PLATFORM_ADMIN" through verbatim).
		if userID != 42 || email != "user@example.com" || role != string(model.SystemRoleSuperAdmin) || orgID != "org-1" {
			t.Fatalf("unexpected context values: userID=%d email=%q role=%q orgID=%q", userID, email, role, orgID)
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Octopus-External-Auth", "Bearer shared-overlay-secret")
	req.Header.Set("X-Octopus-User-ID", "42")
	req.Header.Set("X-Octopus-User-Email", "user@example.com")
	req.Header.Set("X-Octopus-User-Role", "PLATFORM_ADMIN")
	req.Header.Set("X-Octopus-Org-ID", "org-1")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected trusted external auth to pass, got %d", resp.Code)
	}
}

// TestJWTAuthMapsOverlayRoles asserts the trusted-overlay role translation:
// ORG_USER -> USER, and any unrecognized role name degrades to USER
// (least-privilege) rather than escalating or rejecting.
func TestJWTAuthMapsOverlayRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		header string
		want   string
	}{
		{"ORG_USER", string(model.SystemRoleUser)},
		{"org_user", string(model.SystemRoleUser)}, // case-insensitive
		{"org_admin", string(model.SystemRoleUser)}, // unknown -> USER
		{"", string(model.SystemRoleUser)},          // empty -> USER
		{"SUPER_ADMIN", string(model.SystemRoleSuperAdmin)}, // already Octopus-shaped
	}
	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			router := gin.New()
			router.Use(JWTAuth(testExternalAuthConfig()))
			router.GET("/protected", func(c *gin.Context) {
				_, _, role, ok := GetCurrentUser(c)
				if !ok {
					t.Fatalf("expected external user in context")
				}
				if role != tc.want {
					t.Fatalf("role %q should map to %q, got %q", tc.header, tc.want, role)
				}
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("X-Octopus-External-Auth", "Bearer shared-overlay-secret")
			req.Header.Set("X-Octopus-User-ID", "42")
			req.Header.Set("X-Octopus-User-Email", "user@example.com")
			if tc.header != "" {
				req.Header.Set("X-Octopus-User-Role", tc.header)
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("expected trusted external auth to pass, got %d", resp.Code)
			}
		})
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

func TestRequireAdminRejectsMalformedRoleWithoutPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", 123)
		c.Next()
	})
	router.GET("/admin", RequireAdmin(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected malformed role to be forbidden, got %d", resp.Code)
	}
}

func TestGetCurrentUserRejectsMalformedContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", "not-a-uint")
	c.Set("email", "user@example.com")
	c.Set("role", string(model.SystemRoleUser))

	if _, _, _, ok := GetCurrentUser(c); ok {
		t.Fatal("expected malformed user_id to be rejected")
	}
}
