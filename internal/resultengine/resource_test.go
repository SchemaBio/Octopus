package resultengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/writer"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

type wideFixture struct {
	Position string `parquet:"name=Position, type=BYTE_ARRAY, convertedtype=UTF8"`
	Note     string `parquet:"name=Note, type=BYTE_ARRAY, convertedtype=UTF8"`
}

func TestWideHeapFallbackAndExportCancellation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "wide.parquet")
	f, err := local.NewLocalFileWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := writer.NewParquetWriter(f, new(wideFixture), 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 299; i >= 0; i-- {
		if err = w.Write(wideFixture{Position: fmt.Sprint(i), Note: strings.Repeat("wide", 6000)}); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.WriteStop(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	data, _ := os.ReadFile(path)
	h := sha256.Sum256(data)
	temp := t.TempDir()
	e := New(root, temp, temp)
	q := Request{Table: "snv-indel", FilePath: path, DatasetID: strings.Repeat("a", 64), ObjectHash: hex.EncodeToString(h[:]), Limit: 200, Offset: 100, Sort: "Position"}
	r, err := e.Query(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Items) != 200 || r.Items[0]["Position"] != "100" || r.Items[199]["Position"] != "299" || r.Items[0]["__row_id"] == "" {
		t.Fatal("heap fallback changed page or row identity")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := e.Export(ctx, q); done <- err }()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			entries, _ := os.ReadDir(temp)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "octopus-effective-") {
					cancel()
					goto cancelled
				}
			}
		case err := <-done:
			t.Fatalf("export ended before cancellation: %v", err)
		case <-deadline:
			t.Fatal("export did not start")
		}
	}
cancelled:
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(temp)
	if len(entries) != 0 {
		t.Fatal("cancelled export retained files")
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
