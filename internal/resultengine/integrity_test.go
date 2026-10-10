package resultengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCorruptSourcesAndCancelledPreparation(t *testing.T) {
	bytes, err := os.ReadFile("testdata/snappy.parquet")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	engine := New(root, root, root)
	for name, data := range map[string][]byte{"truncated": bytes[:len(bytes)-8], "broken-page": append([]byte{}, bytes...)} {
		if name == "broken-page" {
			for i := 4; i < 90; i++ {
				data[i] = 0xff
			}
		}
		path := filepath.Join(root, name+".parquet")
		os.WriteFile(path, data, 0600)
		hash := sha256.Sum256(data)
		q := Request{Table: "snv-indel", FilePath: path, DatasetID: strings.Repeat("a", 64), ObjectHash: hex.EncodeToString(hash[:]), Limit: 20}
		if _, err = engine.Query(context.Background(), q); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	path, _ := filepath.Abs("testdata/snappy.parquet")
	safeRoot := filepath.Dir(path)
	engine = New(safeRoot, root, root)
	q := Request{Table: "snv-indel", FilePath: path, DatasetID: strings.Repeat("a", 64), ObjectHash: strings.Repeat("b", 64), Limit: 20}
	if _, err = engine.Query(context.Background(), q); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	hash := sha256.Sum256(bytes)
	q.ObjectHash = hex.EncodeToString(hash[:])
	q.RowCount = 9
	if _, err = engine.Query(context.Background(), q); err == nil {
		t.Fatal("row mismatch accepted")
	}
	q.RowCount = 0
	engine.prepare <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := engine.Prepare(ctx, q); done <- err }()
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked prepare ignored cancellation")
	}
	<-engine.prepare
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatal("partial assessment retained")
		}
	}
}
func TestBoundedExternalMergeStableAndCleanup(t *testing.T) {
	root := t.TempDir()
	s, err := newSorter(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	path := s.dir
	for i := 199; i >= 0; i-- {
		n := float64(i % 4)
		if err = s.add(sortEntry{Ordinal: int64(i), Key: sortKey{Number: &n}, Row: map[string]interface{}{"Position": int64(9007199254740993)}}); err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			if err = s.flush(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(s.runs) <= 32 {
		t.Fatal("fixture did not exercise merge passes")
	}
	previous := sortEntry{Ordinal: -1}
	count := 0
	err = s.each(func(v sortEntry) error {
		if count > 0 && !s.less(previous, v) {
			t.Fatal("unstable merge")
		}
		n := v.Row["Position"]
		if n != int64(9007199254740993) && n != json.Number("9007199254740993") {
			t.Fatal("spill lost integer precision")
		}
		previous = v
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 200 {
		t.Fatal(count)
	}
	s.close()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("spill retained")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s, _ = newSorter(ctx, root, false)
	s.add(sortEntry{Ordinal: 1})
	s.flush()
	cancel()
	if err = s.each(func(sortEntry) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("sort ignored cancellation")
	}
	s.close()
}
