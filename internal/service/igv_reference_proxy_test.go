package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestIGVReferenceProxyChecksStorageRangeAndAttempt(t *testing.T) {
	ignoredRange := false
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if !strings.HasSuffix(r.URL.Path, "/database/hg38/Homo_sapiens.GRCh38.dna.primary_assembly.fa") {
			t.Errorf("unexpected reference object %s", r.URL.Path)
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "1000")
			return
		}
		if ignoredRange {
			w.Header().Set("Content-Length", "1000")
			return
		}
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("unexpected range %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Content-Range", "bytes 0-3/1000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("ACGT"))
	}))
	defer server.Close()
	cfg := &config.Config{Storage: config.StorageConfig{S3Endpoint: server.URL, S3Region: "test", S3UsePathStyle: true, S3AccessKey: "test", S3SecretKey: "test", CVMReferenceBucket: "references"}}
	svc := NewResultService(cfg)
	task := &model.Task{UUID: "task", ExecutionAttemptID: "new", InputJSON: `{"reference_genome":"hg38"}`}
	if _, err := svc.OpenIGVReference(context.Background(), task, "fasta", "old", "bytes=0-3"); !errors.Is(err, ErrIGVEvidenceChanged) || requests != 0 {
		t.Fatal("stale attempt contacted storage")
	}
	stream, err := svc.OpenIGVReference(context.Background(), task, "fasta", "new", "bytes=0-3")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(stream.Body)
	stream.Body.Close()
	if err != nil || string(data) != "ACGT" || stream.ContentRange != "bytes 0-3/1000" {
		t.Fatal("bounded storage response not preserved")
	}
	ignoredRange = true
	if _, err := svc.OpenIGVReference(context.Background(), task, "fasta", "new", "bytes=0-3"); err == nil {
		t.Fatal("ignored storage range accepted")
	}
}

func TestIGVReferenceRangeBounds(t *testing.T) {
	for _, tc := range []struct {
		value      string
		size       int64
		fasta      bool
		valid      bool
		start, end int64
	}{
		{"bytes=0-63", 3153507220, true, true, 0, 63},
		{"bytes=999-2000", 1000, true, true, 999, 999},
		{"bytes=900-", 1000, true, true, 900, 999},
		{"", 2743, false, true, 0, 2742},
		{"", 3153507220, true, false, 0, 0},
		{"bytes=0-", 3153507220, true, false, 0, 0},
		{"bytes=0-8388608", 3153507220, true, false, 0, 0},
		{"bytes=-64", 1000, true, false, 0, 0},
		{"bytes=0-1,3-4", 1000, true, false, 0, 0},
		{"bytes=1000-1001", 1000, true, false, 0, 0},
		{"bytes=2-1", 1000, true, false, 0, 0},
	} {
		a, b, err := igvReferenceRange(tc.value, tc.size, tc.fasta)
		if (err == nil) != tc.valid || (tc.valid && (a != tc.start || b != tc.end)) {
			t.Fatalf("range %q: got %d,%d,%v", tc.value, a, b, err)
		}
	}
}

func TestIGVReferenceProxyUsesExecutionIdentity(t *testing.T) {
	cfg := &config.Config{IGV: config.IGVConfig{ReferenceProxyBaseURL: "https://yijian.example/api/v1/octopus"}}
	task := &model.Task{UUID: "task-1", ExecutionAttemptID: "attempt-2", InputJSON: `{"reference_genome":"hg19"}`}
	r := igvReferenceForTask(cfg, task)
	if !r.Available || r.FASTAURL != "https://yijian.example/api/v1/octopus/tasks/task-1/results/igv/reference/fasta?attempt=attempt-2" {
		t.Fatalf("unexpected reference %#v", r)
	}
	task.InputJSON = `{"reference_genome":"unknown"}`
	if igvReferenceForTask(cfg, task).Available {
		t.Fatal("unknown reference accepted")
	}
}
