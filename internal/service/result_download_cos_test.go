package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
)

type downloadRoundTripper func(*http.Request) (*http.Response, error)

func (f downloadRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDirectCOSDownloadScopesAndSignsTrafficLimit(t *testing.T) {
	expires := time.Now().Add(30 * time.Minute)
	client := &http.Client{Transport: downloadRoundTripper(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Policy          string
			DurationSeconds int
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		var policy struct {
			Statement []struct {
				Action    []string
				Resource  []string
				Condition map[string]interface{}
			}
		}
		if err := json.Unmarshal([]byte(payload.Policy), &policy); err != nil {
			t.Fatal(err)
		}
		if len(policy.Statement) != 1 {
			t.Fatal("unexpected policy")
		}
		s := policy.Statement[0]
		if len(s.Condition) != 0 || len(s.Action) != 1 || s.Action[0] != "name/cos:GetObject" || len(s.Resource) != 1 || s.Resource[0] != "qcs::cos:ap-guangzhou:uid/1234567890:bucket-1234567890/path/report.bam" {
			t.Fatalf("overbroad or IP-bound policy: %+v", s)
		}
		if payload.DurationSeconds > 1830 || payload.DurationSeconds <= 0 {
			t.Fatalf("invalid STS duration: %d", payload.DurationSeconds)
		}
		body, _ := json.Marshal(map[string]interface{}{"Response": map[string]interface{}{"Credentials": map[string]string{"TmpSecretId": "temporary-id", "TmpSecretKey": "temporary-secret", "Token": "test-token"}, "ExpiredTime": expires.Unix() + 30}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}
	cfg := config.StorageConfig{S3AccessKey: "permanent-id", S3SecretKey: "permanent-secret", S3Bucket: "bucket-1234567890", S3Region: "ap-guangzhou"}
	link, err := directCOSDownloadWithClient(context.Background(), cfg, "path/report.bam", "报告 sample.bam", expires, client)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("x-cos-traffic-limit") != "83886080" || !strings.Contains(q.Get("q-url-param-list"), "x-cos-traffic-limit") {
		t.Fatal("traffic limit is not signed")
	}
	if !strings.HasSuffix(q.Get("q-sign-time"), ";"+strconv.FormatInt(expires.Unix(), 10)) {
		t.Fatal("wrong signature deadline")
	}
	if q.Get("q-ak") != "temporary-id" || strings.Contains(link, "temporary-secret") || strings.Contains(link, "permanent-secret") {
		t.Fatal("credentials leaked")
	}
	if u.Host != "bucket-1234567890.cos.ap-guangzhou.myqcloud.com" || u.Path != "/path/report.bam" {
		t.Fatal("wrong object")
	}
	for _, invalid := range []string{"path/*", "path/?"} {
		if _, err := directCOSDownloadWithClient(context.Background(), cfg, invalid, "file", expires, client); err == nil {
			t.Fatal("wildcard accepted")
		}
	}
	if _, err := directCOSDownloadWithClient(context.Background(), cfg, "file", "file", time.Now().Add(time.Hour), client); err == nil {
		t.Fatal("long signature accepted")
	}
}
