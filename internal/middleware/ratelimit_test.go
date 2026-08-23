package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestIPRateLimitRejectsAfterLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(IPRateLimit(1, time.Minute))
	router.GET("/limited", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req1 := httptest.NewRequest(http.MethodGet, "/limited", nil)
	resp1 := httptest.NewRecorder()
	router.ServeHTTP(resp1, req1)
	if resp1.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", resp1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/limited", nil)
	resp2 := httptest.NewRecorder()
	router.ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d", resp2.Code)
	}
}

func TestIPRateLimitIgnoresForwardedIPFromUntrustedClient(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	router.Use(IPRateLimit(1, time.Minute))
	router.GET("/limited", func(c *gin.Context) { c.Status(http.StatusOK) })

	req1 := httptest.NewRequest(http.MethodGet, "/limited", nil)
	req1.RemoteAddr = "192.0.2.10:1234"
	req1.Header.Set("X-Forwarded-For", "198.51.100.1")
	resp1 := httptest.NewRecorder()
	router.ServeHTTP(resp1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/limited", nil)
	req2.RemoteAddr = "192.0.2.10:5678"
	req2.Header.Set("X-Forwarded-For", "203.0.113.2")
	resp2 := httptest.NewRecorder()
	router.ServeHTTP(resp2, req2)

	if resp1.Code != http.StatusOK || resp2.Code != http.StatusTooManyRequests {
		t.Fatalf("status codes = %d, %d; spoofed forwarded IP bypassed rate limit", resp1.Code, resp2.Code)
	}
}
