package exampledata

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/writer"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name string, role Role, raw []byte, compressed bool) Object {
	t.Helper()
	var buf bytes.Buffer
	if compressed {
		g := gzip.NewWriter(&buf)
		_, _ = g.Write(raw)
		if err := g.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		buf.Write(raw)
	}
	file := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(name, ".parquet") {
		f, err := local.NewLocalFileWriter(file)
		if err != nil {
			t.Fatal(err)
		}
		type row struct {
			Position int64 `parquet:"name=Position, type=INT64"`
		}
		w, err := writer.NewParquetWriter(f, new(row), 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Write(row{Position: 1}); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteStop(); err != nil {
			t.Fatal(err)
		}
		f.Close()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return object(filepath.ToSlash(name), role, string(data))
	}
	return object(filepath.ToSlash(name), role, buf.String())
}
func contentFixture(t *testing.T) (string, Manifest) {
	root := t.TempDir()
	m := validManifest()
	m.Objects = []Object{writeFixture(t, root, "r1.fastq.gz", Read1, []byte("@read/1\nACGT\n+\n!!!!\n"), true), writeFixture(t, root, "r2.fastq.gz", Read2, []byte("@read/2\nTGCA\n+\n!!!!\n"), true), writeFixture(t, root, "sample.bam", BAM, []byte{'B', 'A', 'M', 1, 0, 0, 0, 0, 1, 0, 0, 0, 5, 0, 0, 0, 'c', 'h', 'r', '1', 0, 100, 0, 0, 0}, true), writeFixture(t, root, "sample.bam.bai", BAMIndex, []byte{'B', 'A', 'I', 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false), writeFixture(t, root, "result.parquet", Result, []byte("PAR1"), false)}
	m.Outputs = json.RawMessage(`{"summary":{"qc_result":{},"bam":"example://objects/sample.bam","bai":"example://objects/sample.bam.bai","variants":"example://objects/result.parquet"}}`)
	return root, m
}
func TestExampleContentRejectsMismatchedReadsAndOpaqueContainers(t *testing.T) {
	root, m := contentFixture(t)
	if err := m.ValidateFiles(root); err != nil {
		t.Fatal(err)
	}
	m.Objects[1] = writeFixture(t, root, "r2.fastq.gz", Read2, []byte("@different/2\nTGCA\n+\n!!!!\n"), true)
	if err := m.ValidateFiles(root); err == nil {
		t.Fatal("mismatched pair accepted")
	}
	root, m = contentFixture(t)
	m.Objects[4] = writeFixture(t, root, "report.txt", Result, []byte{'P', 'K', 3, 4, 0, 0}, false)
	if err := m.ValidateFiles(root); err == nil {
		t.Fatal("renamed ZIP accepted")
	}
}
func TestExampleOutputsAreRewrittenByExactReference(t *testing.T) {
	refs := map[string]string{"sample.bam": "cos://bucket/organizations/target/workflows/task/sample.bam"}
	got, err := RewriteOutputs([]byte(`{"bam":"example://objects/sample.bam","label":"sample"}`), refs)
	if err != nil || !bytes.Contains(got, []byte(refs["sample.bam"])) {
		t.Fatalf("rewrite failed: %s %v", got, err)
	}
	for _, raw := range []string{`{"bam":"example://objects/missing.bam"}`, `{"bam":"cos://bucket/organizations/source/private.bam"}`, `{"bam":"/source/private.bam"}`, `{} {}`} {
		if _, err := RewriteOutputs([]byte(raw), refs); err == nil {
			t.Fatalf("unsafe outputs accepted: %s", raw)
		}
	}
}

func TestExampleBAMCannotHideInResultZIPFieldsOrEscapeRetentionDiscovery(t *testing.T) {
	for _, raw := range []string{
		`{"summary":{"qc_result":{},"snp_indel":"example://objects/sample.bam","bai":"example://objects/sample.bam.bai","variants":"example://objects/result.parquet"}}`,
		`{"summary":{"qc_result":{},"variants":"example://objects/result.parquet"}}`,
	} {
		root, m := contentFixture(t)
		m.Outputs = json.RawMessage(raw)
		if err := m.ValidateFiles(root); err == nil {
			t.Fatal("unsafe alignment output accepted")
		}
	}
}
