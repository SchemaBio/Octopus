package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/gin-gonic/gin"
)

func TestCORSAllowsAIAssistanceIdempotencyPreflight(t *testing.T) {
	router := gin.New()
	router.Use(CORS(&config.ServerConfig{AllowedOrigins: "http://localhost:3000"}))
	router.POST("/api/v1/ai-assistance/requests", func(c *gin.Context) { c.Status(http.StatusAccepted) })
	for _, origin := range []string{"http://localhost:3000", "https://untrusted.example"} {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/ai-assistance/requests", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type,idempotency-key,x-csrf-token")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusNoContent {
			t.Fatal(response.Code)
		}
		if origin != "http://localhost:3000" {
			if response.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("untrusted origin allowed")
			}
			continue
		}
		if response.Header().Get("Access-Control-Allow-Origin") != origin || response.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatal("credentialed browser request blocked")
		}
		allowed := map[string]bool{}
		for _, header := range strings.Split(response.Header().Get("Access-Control-Allow-Headers"), ",") {
			allowed[strings.ToLower(strings.TrimSpace(header))] = true
		}
		for _, header := range []string{"content-type", "idempotency-key", "x-csrf-token"} {
			if !allowed[header] {
				t.Fatalf("browser preflight blocks %s", header)
			}
		}
	}
}
