package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
)

func testRouterConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Mode:           "release",
			AllowedOrigins: "http://example.com",
		},
		JWT: config.JWTConfig{
			Secret:          "test-secret-with-at-least-32-characters",
			Issuer:          "octopus-test",
			ExpireDuration:  time.Hour,
			RefreshDuration: 24 * time.Hour,
		},
	}
}

func TestReportTemplateCreateRequiresAdmin(t *testing.T) {
	cfg := testRouterConfig()

	token, _, _, err := service.NewJWTService(cfg).GenerateToken(&model.User{
		ID:         1,
		Email:      "user@example.com",
		SystemRole: model.SystemRoleUser,
	})
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	router := New(cfg)
	body := strings.NewReader(`{"name":"template","apiEndpoint":"https://example.com/report"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/report-templates", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin template creation to be forbidden, got %d", resp.Code)
	}
}

func TestUserApprovalRoutesRequireAdmin(t *testing.T) {
	cfg := testRouterConfig()
	token, _, _, err := service.NewJWTService(cfg).GenerateToken(&model.User{
		ID:         1,
		Email:      "user@example.com",
		SystemRole: model.SystemRoleUser,
		IsActive:   true,
	})
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	router := New(cfg)
	for _, path := range []string{
		"/api/v1/users/pending",
		"/api/v1/users/1/approve",
		"/api/v1/users/1/reject",
	} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/approve") || strings.HasSuffix(path, "/reject") {
			method = http.MethodPost
		}
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusForbidden {
			t.Fatalf("expected non-admin %s %s to be forbidden, got %d", method, path, resp.Code)
		}
	}
}

func TestLegacyAuthRoutesRedirectWith308(t *testing.T) {
	router := New(testRouterConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(`{"email":"u@example.com","password":"secret"}`))
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected legacy auth route to return 308, got %d", resp.Code)
	}
	if location := resp.Header().Get("Location"); location != "/api/v1/auth/login" {
		t.Fatalf("unexpected redirect location: %q", location)
	}
}

func TestTenantStorageEndpointRejectsInvalidInternalSecret(t *testing.T) {
	cfg := testRouterConfig()
	cfg.ExternalAuth = config.ExternalAuthConfig{
		Enabled: true, SharedSecret: "test-internal-secret-with-at-least-32-characters",
	}
	router := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/storage/tenants", strings.NewReader(`{"org_id":"f83165ee-e23c-4bf1-a42e-78ac39c6f1ba"}`))
	req.Header.Set("Authorization", "Bearer wrong-secret")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid internal secret to be rejected, got %d", resp.Code)
	}
}

func TestWorkflowTemplateCatalogRequiresAuthentication(t *testing.T) {
	router := New(testRouterConfig())
	for _, path := range []string{
		"/api/v1/templates",
		"/api/v1/templates/germline_single",
		"/api/v1/templates/germline_single/inputs",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("expected unauthenticated GET %s to be rejected, got %d", path, resp.Code)
		}
	}
}
