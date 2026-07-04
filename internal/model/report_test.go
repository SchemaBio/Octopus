package model

import "testing"

func TestReportTemplateAdminResponseDoesNotExposeAPIKey(t *testing.T) {
	resp := (&ReportTemplate{
		Name:        "clinical",
		APIEndpoint: "https://reports.example.com/generate",
		APIKey:      "secret-api-key",
	}).ToAdminResponse()

	if !resp.HasAPIKey {
		t.Fatal("expected admin response to indicate an API key is configured")
	}
}
