package resultengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiskBudgetFailureCleansSpills(t *testing.T) {
	root := t.TempDir()
	s, err := newSorter(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	s.disk.limit = 64
	s.add(sortEntry{Ordinal: 1, CSV: strings.Repeat("x", 128)})
	if err = s.flush(); err == nil {
		t.Fatal("disk budget ignored")
	}
	s.close()
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("failed spill retained")
	}
	var b bytes.Buffer
	w := budgetWriter{writer: &b, budget: &diskBudget{limit: 3}}
	if _, err = w.Write([]byte("four")); err == nil || b.Len() != 0 {
		t.Fatal("budget allowed partial oversized write")
	}
}
func TestDenseOverlayAndLargeOffset(t *testing.T) {
	root, _ := filepath.Abs("testdata")
	data, _ := os.ReadFile(filepath.Join(root, "snappy.parquet"))
	h := sha256.Sum256(data)
	q := Request{Table: "snv-indel", FilePath: filepath.Join(root, "snappy.parquet"), DatasetID: strings.Repeat("a", 64), ObjectHash: hex.EncodeToString(h[:]), Limit: 2, Sort: "Position", Offset: 1500}
	for i := 0; i < 100000; i++ {
		q.Overlays = append(q.Overlays, Overlay{RowID: RowID(q.DatasetID, q.ObjectHash, int64(i)), Payload: map[string]interface{}{"reviewed": true}, Version: 1})
	}
	temp := t.TempDir()
	e := New(root, temp, temp)
	r, err := e.Query(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 6 || len(r.Items) != 0 {
		t.Fatal(r)
	}
	entries, _ := os.ReadDir(temp)
	if len(entries) != 0 {
		t.Fatal("large offset spill retained")
	}
}
func BenchmarkQuery(b *testing.B) { benchmarkEngine(b, false) }
func BenchmarkSort(b *testing.B)  { benchmarkEngine(b, true) }
func benchmarkEngine(b *testing.B, sorted bool) {
	root, _ := filepath.Abs("testdata")
	data, _ := os.ReadFile(filepath.Join(root, "snappy.parquet"))
	h := sha256.Sum256(data)
	temp := b.TempDir()
	e := New(root, temp, temp)
	q := Request{Table: "snv-indel", FilePath: filepath.Join(root, "snappy.parquet"), DatasetID: strings.Repeat("a", 64), ObjectHash: hex.EncodeToString(h[:]), Limit: 2}
	if sorted {
		q.Sort = "Position"
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Query(context.Background(), q); err != nil {
			b.Fatal(err)
		}
	}
}
