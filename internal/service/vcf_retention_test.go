package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestVCFAssetDeletionErasesVersionsAndMultipartWithoutDeletingNeighbors(t *testing.T) {
	key := "organizations/org/uploads/job/sample.vcf.gz"
	var deleted []string
	versions := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.Method == http.MethodDelete {
			if r.URL.Path != "/test/"+key {
				t.Errorf("neighbor object deletion: %s", r.URL.Path)
			}
			deleted = append(deleted, r.URL.RawQuery)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Query().Has("versions") {
			versions++
			if versions == 1 {
				w.Write([]byte(`<ListVersionsResult><IsTruncated>false</IsTruncated><Version><Key>` + key + `</Key><VersionId>v1</VersionId></Version><Version><Key>` + key + `.backup</Key><VersionId>neighbor</VersionId></Version><DeleteMarker><Key>` + key + `</Key><VersionId>marker</VersionId></DeleteMarker></ListVersionsResult>`))
			} else {
				w.Write([]byte(`<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`))
			}
			return
		}
		if r.URL.Query().Has("uploads") {
			w.Write([]byte(`<ListMultipartUploadsResult><IsTruncated>false</IsTruncated><Upload><Key>` + key + `</Key><UploadId>unfinished</UploadId></Upload><Upload><Key>` + key + `.backup</Key><UploadId>neighbor</UploadId></Upload></ListMultipartUploadsResult>`))
			return
		}
		w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`))
	}))
	defer server.Close()
	s := &DataAssetService{cfg: &config.Config{Edition: "saas", Storage: config.StorageConfig{S3Endpoint: server.URL, S3UsePathStyle: true, S3Region: "test", S3Bucket: "test", S3AccessKey: "test", S3SecretKey: "test"}}}
	if err := s.deleteStoredObject(context.Background(), &model.DataAsset{Provider: model.UploadProviderS3, ReadType: model.ReadTypeVCF, StorageKey: key}); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 3 || !strings.Contains(strings.Join(deleted, "\n"), "versionId=v1") || !strings.Contains(strings.Join(deleted, "\n"), "versionId=marker") || !strings.Contains(strings.Join(deleted, "\n"), "uploadId=unfinished") || versions < 2 {
		t.Fatalf("VCF erasure incomplete: versions=%d deletes=%v", versions, deleted)
	}
}
