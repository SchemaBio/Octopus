package resultengine

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func TestSchemaAndCompressedBatches(t *testing.T) {
	for _, name := range []string{"snappy", "gzip", "zstd", "uncompressed", "empty", "numeric"} {
		t.Run(name, func(t *testing.T) {
			r, err := Open(context.Background(), "testdata", filepath.Join("testdata", name+".parquet"))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if len(r.Columns()) == 0 {
				t.Fatal("schema lost")
			}
			rows := int64(0)
			for {
				batch, err := r.ReadBatch(2)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				rows += int64(len(batch))
				if len(batch) > 2 {
					t.Fatal("unbounded batch")
				}
				if name == "snappy" && rows == 2 {
					if batch[0]["Gene"] != "GENE1" || batch[0]["Chromosome"] != "chr2" {
						t.Fatalf("names changed: %#v", batch[0])
					}
				}
				if name == "numeric" && rows == 2 {
					if batch[0]["Position"] != int64(9007199254740993) {
						t.Fatalf("integer precision lost: %#v", batch[0])
					}
				}
			}
			if rows != r.NumRows() {
				t.Fatalf("got %d want %d", rows, r.NumRows())
			}
		})
	}
}

func TestReaderCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r, err := Open(ctx, "testdata", "testdata/snappy.parquet")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cancel()
	if _, err = r.ReadBatch(2); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}
