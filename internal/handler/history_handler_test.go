package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/gin-gonic/gin"
)

func TestHistoryApplyScopeUsesOrgForNonAdminOverlayUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", uint(42))
	c.Set("email", "user@example.com")
	c.Set("role", string(model.SystemRoleUser))
	c.Set("org_id", "org-1")

	query := &model.HistoryListQuery{}
	h := &HistoryHandler{}
	if !h.applyScope(c, query) {
		t.Fatal("expected scope to apply")
	}
	if query.IncludeAll {
		t.Fatal("non-admin must not include all history")
	}
	if query.ExternalOrgID != "org-1" || query.CreatedBy != 0 {
		t.Fatalf("unexpected scoped query: %+v", query)
	}
}

func TestHistoryApplyScopeUsesCreatedByForStandaloneUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", uint(42))
	c.Set("email", "user@example.com")
	c.Set("role", string(model.SystemRoleUser))

	query := &model.HistoryListQuery{}
	h := &HistoryHandler{}
	if !h.applyScope(c, query) {
		t.Fatal("expected scope to apply")
	}
	if query.IncludeAll || query.CreatedBy != 42 || query.ExternalOrgID != "" {
		t.Fatalf("unexpected scoped query: %+v", query)
	}
}

func TestHistoryApplyScopeAllowsSuperAdminAndClampsPageSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/history?pageSize=10000", nil)
	c.Request = req
	c.Set("user_id", uint(1))
	c.Set("email", "admin@example.com")
	c.Set("role", string(model.SystemRoleSuperAdmin))

	h := &HistoryHandler{}
	query := h.bindQuery(c)
	if query.PageSize != 100 {
		t.Fatalf("expected page size clamp to 100, got %d", query.PageSize)
	}
	if !h.applyScope(c, query) {
		t.Fatal("expected scope to apply")
	}
	if !query.IncludeAll || query.CreatedBy != 0 || query.ExternalOrgID != "" {
		t.Fatalf("unexpected admin scoped query: %+v", query)
	}
}
