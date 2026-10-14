package middleware

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResearchEditionBlocksDirectFeatureAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	paths := []string{"/v1/history/sync", "/v1/history/focuses/id", "/v1/report-templates", "/v1/tasks/id/reports/upload", "/v1/tasks/id/reports/r/download-url", "/v1/tasks/id/report-preview", "/v1/tasks/id/report-generations"}
	for _, edition := range []string{"opensource", "saas"} {
		r := gin.New()
		r.Use(ResearchPolicy(&config.Config{Edition: edition}))
		r.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
		for _, path := range paths {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			expected := http.StatusOK
			if edition == "saas" {
				expected = http.StatusNotFound
			}
			if w.Code != expected {
				t.Fatalf("%s %s status %d", edition, path, w.Code)
			}
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/v1/tasks/id/results/downloads", nil))
		if w.Code != 200 {
			t.Fatal("analysis downloads blocked")
		}
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/v1/tasks/id/results/rows/snv-indel/row/history", nil))
		if w.Code != 200 {
			t.Fatal("variant operation history blocked")
		}
	}
}
