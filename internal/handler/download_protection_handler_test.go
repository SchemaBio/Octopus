package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestDownloadProtectionAdminRequiresElevatedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{"", "USER", "PLATFORM_ADMIN"} {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("role", role) })
		reached := false
		r.GET("/admin/download-protection/grants", middleware.RequireAdmin(), func(c *gin.Context) { reached = true; c.Status(200) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/download-protection/grants", nil))
		if reached || w.Code != http.StatusForbidden {
			t.Fatalf("unverified elevated identity accepted: %s", role)
		}
	}
}
