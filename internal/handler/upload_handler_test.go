package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/gin-gonic/gin"
)

func TestUploadFileAuditRejectsNonSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/upload/files", nil)
	context.Set("user_id", uint(42))
	context.Set("email", "user@example.test")
	context.Set("role", string(model.SystemRoleUser))

	(&UploadHandler{}).ListFiles(context)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ListFiles status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}
