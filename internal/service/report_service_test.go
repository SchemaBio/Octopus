package service

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/bioinfo/schema-platform/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestValidateReportAPIEndpointRejectsPrivateIP(t *testing.T) {
	err := validateReportAPIEndpointWithResolver("https://reports.example.com/generate", func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.10")}, nil
	})
	if err == nil {
		t.Fatal("expected private IP endpoint to be rejected")
	}
}

func TestValidateReportAPIEndpointRejectsUserInfo(t *testing.T) {
	err := validateReportAPIEndpointWithResolver("https://user:pass@reports.example.com/generate", func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	if err == nil {
		t.Fatal("expected endpoint with user info to be rejected")
	}
}

func TestValidateReportAPIEndpointRejectsHTTP(t *testing.T) {
	err := validateReportAPIEndpointWithResolver("http://reports.example.com/generate", func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	if err == nil {
		t.Fatal("expected non-HTTPS endpoint to be rejected")
	}
}

func TestValidateReportAPIEndpointAcceptsPublicHTTPS(t *testing.T) {
	err := validateReportAPIEndpointWithResolver("https://reports.example.com/generate", func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	if err != nil {
		t.Fatalf("expected public HTTPS endpoint to be accepted: %v", err)
	}
}

func TestValidateReportAPIEndpointResolverError(t *testing.T) {
	err := validateReportAPIEndpointWithResolver("https://reports.example.com/generate", func(string) ([]net.IP, error) {
		return nil, fmt.Errorf("resolver unavailable")
	})
	if err == nil {
		t.Fatal("expected resolver error to reject endpoint")
	}
}

func TestGenerateReportDownloadStreamsAPIResponse(t *testing.T) {
	var gotAuthorization string
	var gotContentType string
	var gotAccept string
	var gotBody string
	svc := &ReportService{
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotAuthorization = req.Header.Get("Authorization")
			gotContentType = req.Header.Get("Content-Type")
			gotAccept = req.Header.Get("Accept")
			body, _ := io.ReadAll(req.Body)
			gotBody = string(body)
			return &http.Response{
				StatusCode:    http.StatusOK,
				Header:        http.Header{"Content-Type": []string{"application/pdf"}, "Content-Disposition": []string{`attachment; filename="case-report.pdf"`}},
				Body:          io.NopCloser(strings.NewReader("report-bytes")),
				ContentLength: int64(len("report-bytes")),
				Request:       req,
			}, nil
		})},
	}

	download, err := svc.generateReportDownload(
		t.Context(),
		&model.ReportTemplate{Name: "clinical", APIEndpoint: "https://8.8.8.8/generate", APIKey: "secret"},
		&model.Task{UUID: "task-1", Name: "Task One", SampleID: "sample-1", Pipeline: "wes"},
		&model.ReportCreateRequest{Name: "ignored-name", TemplateName: "clinical"},
		"user@example.com",
	)
	if err != nil {
		t.Fatalf("generateReportDownload returned error: %v", err)
	}
	defer download.Body.Close()

	if gotAuthorization != "Bearer secret" {
		t.Fatalf("unexpected authorization header: %q", gotAuthorization)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected content type header: %q", gotContentType)
	}
	if !strings.Contains(gotAccept, "application/pdf") {
		t.Fatalf("expected Accept to include application/pdf, got %q", gotAccept)
	}
	if !strings.Contains(gotBody, `"taskId":"task-1"`) || !strings.Contains(gotBody, `"createdBy":"user@example.com"`) {
		t.Fatalf("unexpected report API payload: %s", gotBody)
	}
	if download.FileName != "case-report.pdf" {
		t.Fatalf("unexpected filename: %q", download.FileName)
	}
	if download.ContentType != "application/pdf" {
		t.Fatalf("unexpected download content type: %q", download.ContentType)
	}
	data, _ := io.ReadAll(download.Body)
	if string(data) != "report-bytes" {
		t.Fatalf("unexpected download body: %q", string(data))
	}
}

func TestReportDownloadFileNameFallbackAndSanitize(t *testing.T) {
	got := reportDownloadFileName("../unsafe/name", "template", "task-1", "", "application/pdf")
	if got != "name.pdf" {
		t.Fatalf("unexpected sanitized filename: %q", got)
	}

	got = reportDownloadFileName("", "template", "task-1", "", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if got != "template.xlsx" {
		t.Fatalf("unexpected fallback filename: %q", got)
	}
}

func TestGenerateReportDownloadReturnsAPIError(t *testing.T) {
	svc := &ReportService{
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader("upstream failed")),
				Request:    req,
			}, nil
		})},
	}

	_, err := svc.generateReportDownload(
		t.Context(),
		&model.ReportTemplate{Name: "clinical", APIEndpoint: "https://8.8.8.8/generate"},
		&model.Task{UUID: "task-1"},
		&model.ReportCreateRequest{Name: "report", TemplateName: "clinical"},
		"user@example.com",
	)
	if err == nil || !strings.Contains(err.Error(), "report API returned 502") {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestReportDownloadBodyRejectsChunkedOversize(t *testing.T) {
	body := newMaxBytesReadCloser(io.NopCloser(io.LimitReader(infiniteByteReader{}, maxReportDownloadBytes+1)), maxReportDownloadBytes)
	defer body.Close()

	_, err := io.Copy(io.Discard, body)
	if err == nil {
		t.Fatal("expected oversized chunked response body to fail")
	}
	if !strings.Contains(err.Error(), ErrReportDownloadTooLarge.Error()) {
		t.Fatalf("expected size error, got %v", err)
	}
}

type infiniteByteReader struct{}

func (infiniteByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}
