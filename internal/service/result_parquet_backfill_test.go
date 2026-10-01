package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/reader"
)

func TestControlledBackfillPreservesRowsAndUniqueColumns(t *testing.T) {
	input := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(input, []byte("Chromosome\tPosition\tGene\tGene\tSize(Mb)\nchr1\t100000001\tA\tB\t0.001\nchr2\t2\tC\tD\t.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output, rows, headers, err := convertArchivedTextParquet(input)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(output)
	if rows != 2 || headers[3] != "Gene_2" || headers[4] != "Size_Mb_" {
		t.Fatalf("lost source columns: %d %#v", rows, headers)
	}
	file, err := local.NewLocalFileReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	parquet, err := reader.NewParquetReader(file, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer parquet.ReadStop()
	if parquet.GetNumRows() != 2 {
		t.Fatal("written row count differs")
	}
}

func TestControlledBackfillRejectsMalformedRows(t *testing.T) {
	input := filepath.Join(t.TempDir(), "report.txt")
	_ = os.WriteFile(input, []byte("Chromosome\tPosition\nchr1\t10\textra\n"), 0600)
	if output, _, _, err := convertArchivedTextParquet(input); err == nil {
		os.Remove(output)
		t.Fatal("malformed input silently accepted")
	}
}
