package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResearchDeadlineAndSigningBoundary(t *testing.T) {
	created := time.Date(2026, 3, 8, 1, 0, 0, 0, time.FixedZone("offset", -7*3600))
	deadline := model.TaskRetentionDeadline(created)
	if deadline.Sub(created) != 720*time.Hour || deadline.Location() != time.UTC {
		t.Fatal("expiry must be 720 absolute hours in UTC")
	}
	for _, offset := range []time.Duration{0, time.Second, -time.Millisecond} {
		now := deadline.Add(offset)
		_, err := capRetentionTTL(now, time.Hour, deadline)
		if err == nil {
			t.Fatal("due or subsecond grants accepted")
		}
	}
	now := deadline.Add(-90*time.Second - time.Millisecond)
	ttl, err := capRetentionTTL(now, time.Hour, deadline)
	if err != nil || ttl != 90*time.Second || now.Add(ttl).After(deadline) {
		t.Fatal("grant crosses deadline")
	}
	if researchDeadline(&config.Config{}, created) != nil {
		t.Fatal("open source expiry enabled")
	}
}

func TestPromotedBaselineSurvivesRetryAfterSourceErasure(t *testing.T) {
	source := "organizations/org/workflows/11111111-1111-4111-8111-111111111111/reference.cnn"
	target := "organizations/org/baselines/22222222-2222-4222-8222-222222222222/reference.cnn"
	digest := sha256.Sum256([]byte(source))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "HEAD" || !strings.HasSuffix(r.URL.Path, target) {
			t.Errorf("retry touched erased source: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Length", "123")
		w.Header().Set("X-Amz-Meta-Research-Source", hex.EncodeToString(digest[:]))
		w.Header().Set("X-Amz-Meta-Research-Bytes", "123")
		w.WriteHeader(200)
	}))
	defer server.Close()
	storage, err := newS3Storage(context.Background(), config.StorageConfig{S3Endpoint: server.URL, S3UsePathStyle: true, S3Region: "test", S3Bucket: "test", S3AccessKey: "test", S3SecretKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.promoteBaseline(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("verified resource was recopied")
	}
}

func TestSampleIdentityAcknowledgmentCannotBeBypassed(t *testing.T) {
	for _, edition := range []string{"opensource", "saas"} {
		s := &SampleService{cfg: &config.Config{Edition: edition}}
		for _, req := range []model.SampleCreateRequest{{}, {IdentityPolicyAcknowledged: true}, {IdentityPolicyVersion: "research-identity-v1"}, {IdentityPolicyAcknowledged: true, IdentityPolicyVersion: "old"}} {
			if _, err := s.CreateSample(context.Background(), &req, model.OverlayActor{}); err == nil {
				t.Fatalf("%s missing consent accepted", edition)
			}
		}
	}
}

func TestTaskStorageErasesVersionsMarkersAndMultipart(t *testing.T) {
	task := "11111111-1111-4111-8111-111111111111"
	prefix, _ := retentionPrefix("tenant-a", task)
	var deletes []string
	versionCalls, multipartCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.Method == "DELETE" {
			deletes = append(deletes, r.URL.Path+"?"+r.URL.RawQuery)
			w.WriteHeader(204)
			return
		}
		if r.URL.Query().Has("versions") {
			versionCalls++
			if versionCalls == 1 {
				_, _ = w.Write([]byte(`<ListVersionsResult><IsTruncated>false</IsTruncated><Version><Key>` + prefix + `result</Key><VersionId>v1</VersionId></Version><DeleteMarker><Key>` + prefix + `result</Key><VersionId>marker1</VersionId></DeleteMarker></ListVersionsResult>`))
			} else {
				_, _ = w.Write([]byte(`<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`))
			}
			return
		}
		if r.URL.Query().Has("uploads") {
			multipartCalls++
			if multipartCalls == 1 {
				_, _ = w.Write([]byte(`<ListMultipartUploadsResult><IsTruncated>false</IsTruncated><Upload><Key>` + prefix + `partial</Key><UploadId>unfinished</UploadId></Upload></ListMultipartUploadsResult>`))
			} else {
				_, _ = w.Write([]byte(`<ListMultipartUploadsResult><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>`))
			}
			return
		}
		_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`))
	}))
	defer server.Close()
	storage, err := newS3Storage(context.Background(), config.StorageConfig{S3Endpoint: server.URL, S3UsePathStyle: true, S3Region: "test", S3Bucket: "test", S3AccessKey: "test", S3SecretKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.purgePrefix(context.Background(), prefix); err != nil {
		t.Fatal(err)
	}
	if len(deletes) != 3 || !strings.Contains(strings.Join(deletes, "\n"), "versionId=marker1") || !strings.Contains(strings.Join(deletes, "\n"), "uploadId=unfinished") {
		t.Fatalf("incomplete erasure: %v", deletes)
	}
	if versionCalls < 3 {
		t.Fatal("missing final version inventory")
	}
}

func TestTaskStorageDeletionFailsClosedOnForeignObject(t *testing.T) {
	task := "11111111-1111-4111-8111-111111111111"
	prefix, _ := retentionPrefix("tenant-a", task)
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deleted = true
		}
		_, _ = w.Write([]byte(`<ListVersionsResult><Version><Key>organizations/other/workflows/foreign/result</Key><VersionId>v1</VersionId></Version></ListVersionsResult>`))
	}))
	defer server.Close()
	s, err := newS3Storage(context.Background(), config.StorageConfig{S3Endpoint: server.URL, S3UsePathStyle: true, S3Region: "test", S3Bucket: "test", S3AccessKey: "test", S3SecretKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.purgePrefix(context.Background(), prefix); err == nil || deleted {
		t.Fatal("foreign object deletion permitted")
	}
	for _, org := range []string{"", "../other", "tenant/path"} {
		if _, err := retentionPrefix(org, task); err == nil {
			t.Fatal("unsafe tenant accepted")
		}
	}
}
