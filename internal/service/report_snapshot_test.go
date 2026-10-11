package service

import (
	"context"
	"encoding/json"
	"github.com/SchemaBio/Octopus/internal/model"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReportV2TransmitsImmutableSnapshotAndIdempotencyKey(t *testing.T) {
	svc := &ReportService{http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Idempotency-Key") != "generation-1" {
			t.Fatal("missing idempotency identity")
		}
		var payload map[string]interface{}
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Fatal(e)
		}
		if payload["contract_version"] != "report-snapshot-v2" || payload["report_snapshot_sha256"] != "hash-1" {
			t.Fatal("missing snapshot version")
		}
		if _, exists := payload["result_package_url"]; exists {
			t.Fatal("V2 must not send legacy result package links")
		}
		snap := payload["report_snapshot"].(map[string]interface{})
		if len(snap["reported_variants"].([]interface{})) != 0 {
			t.Fatal("negative report should retain empty report set")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/pdf"}}, Body: io.NopCloser(strings.NewReader("pdf")), ContentLength: 3}, nil
	})}}
	d, e := svc.generateReportDownload(context.Background(), &model.ReportTemplate{APIEndpoint: "https://example.com/report", APIKey: "test", ContractVersion: "report-snapshot-v2"}, &model.Task{UUID: "task"}, &model.ReportCreateRequest{Name: "report", GenerationID: "generation-1", SnapshotSHA256: "hash-1", SnapshotJSON: `{"reported_variants":[]}`}, "actor")
	if e != nil {
		t.Fatal(e)
	}
	d.Body.Close()
}
func TestReportEndpointDoesNotTreatServerFailureAsSuccess(t *testing.T) {
	for _, code := range []int{404, 500} {
		svc := &ReportService{http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("error"))}, nil
		})}}
		if _, e := svc.ValidateTemplateEndpoint(context.Background(), "https://example.com/report", "test"); e == nil {
			t.Fatalf("HTTP %d accepted", code)
		}
	}
}
func TestNoResourceMaintenanceFromOrganizationMembershipAlone(t *testing.T) {
	if (model.OverlayActor{UserID: 2, OrgID: "org"}).ResourceMaintenance(1) {
		t.Fatal("regular member may not edit another creator's resource")
	}
	if !(model.OverlayActor{UserID: 2, OrgID: "org", OrgRole: "ORG_ADMIN"}).ResourceMaintenance(1) {
		t.Fatal("verified organization admin should maintain scoped resources")
	}
}

func TestReportV2RejectsLegacyAndMissingSnapshotsBeforeCallingProvider(t *testing.T) {
	svc := &ReportService{http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("invalid report must not call provider")
		return nil, nil
	})}}
	for _, tc := range []struct{ contract, snapshot string }{
		{"legacy-v1", `{}`}, {"unknown", `{}`}, {model.ReportContractV2, ""}, {model.ReportContractV2, "invalid"},
	} {
		_, err := svc.generateReportDownload(context.Background(), &model.ReportTemplate{APIEndpoint: "https://example.com/report", APIKey: "test", ContractVersion: tc.contract}, &model.Task{UUID: "task"}, &model.ReportCreateRequest{SnapshotJSON: tc.snapshot}, "actor")
		if err == nil {
			t.Fatalf("accepted invalid report: %+v", tc)
		}
	}
}

func TestReportConfigurationAcceptsOnlyV2(t *testing.T) {
	for _, version := range []string{"", model.ReportContractV2} {
		if err := validateReportContract(version); err != nil {
			t.Fatal(err)
		}
	}
	for _, version := range []string{"legacy-v1", "unknown"} {
		svc := &ReportService{}
		if _, err := svc.CreateTemplate(1, &model.ReportTemplateCreateRequest{ContractVersion: version}); err == nil {
			t.Fatal("create accepted unsupported protocol")
		}
		if _, err := svc.UpdateTemplate(1, "template", &model.ReportTemplateUpdateRequest{ContractVersion: version}); err == nil {
			t.Fatal("update accepted unsupported protocol")
		}
	}
}
