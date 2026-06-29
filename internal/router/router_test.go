package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/service"
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
