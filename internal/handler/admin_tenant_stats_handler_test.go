package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseAdminTenantStatsOrgIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("GET", "/api/v1/admin/tenant-stats?org_id=org-a&org_id=org-b,org-a", nil)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request

	ids, err := parseAdminTenantStatsOrgIDs(ctx)
	if err != nil {
		t.Fatalf("parseAdminTenantStatsOrgIDs returned error: %v", err)
	}
	if len(ids) != 2 || ids[0] != "org-a" || ids[1] != "org-b" {
		t.Fatalf("unexpected normalized tenant IDs: %#v", ids)
	}
}

func TestParseAdminTenantStatsOrgIDsCapsRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	query := ""
	for i := 0; i < maxAdminTenantStatsOrgIDs+1; i++ {
		if i > 0 {
			query += "&"
		}
		query += "org_id=org-" + string(rune('a'+(i%26))) + "-" + string(rune('0'+(i/26)))
	}
	request := httptest.NewRequest("GET", "/api/v1/admin/tenant-stats?"+query, nil)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request
	if _, err := parseAdminTenantStatsOrgIDs(ctx); err == nil {
		t.Fatal("expected more-than-100 tenant IDs to be rejected")
	}
}
