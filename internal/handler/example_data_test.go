package handler

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestExampleHandlersRejectSelfHostedBeforeStorageOrDatabase(t *testing.T) {
	h := NewExampleDataHandler(&config.Config{Edition: "opensource"})
	for _, method := range []func(*gin.Context){h.Template, h.Enqueue, h.Status} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/api/v1/internal/example-data/grants", nil)
		method(c)
		if w.Code != 404 {
			t.Fatalf("self-hosted endpoint responded %d", w.Code)
		}
	}
}
