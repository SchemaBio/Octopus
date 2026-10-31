package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

// Fixture generated from synthetic CNR bins through the workflow gene command,
// annotation adapter, and CNVAnno 6ac49dc6db96f8a0289e288525f929e2be0004b5.
func TestCNVGeneMetricsReadActualPipelineFixture(t *testing.T) {
	_, rows, err := readTSV(filepath.Join("testdata", "cnv_gene_v1.tsv"))
	if err != nil || len(rows) != 3 {
		t.Fatalf("read pipeline fixture: %v, %d rows", err, len(rows))
	}
	for index, want := range []float64{1, 2, 4} {
		var result model.CNVExon
		if err := applyCNVGeneMetrics(&result, rows[index]); err != nil {
			t.Fatal(err)
		}
		if result.CopyNumber == nil || *result.CopyNumber != want || result.CopyRatio != nil {
			t.Fatalf("row %d lost absolute CN %v: %+v", index, want, result)
		}
	}
}

func TestCNVGeneImportValidatesAllRowsBeforePersistence(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "cnv_gene_v1.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	fields := strings.Split(lines[2], "\t")
	fields[17] = "unknown-layout"
	lines[2] = strings.Join(fields, "\t")
	path := filepath.Join(t.TempDir(), "gene.cnvanno.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A nil repository ensures this path cannot persist the valid first row
	// before discovering the invalid second row.
	count, err := (&Importer{}).importCNVExons("task", path)
	if err == nil || count != 0 || !strings.Contains(err.Error(), "row 3") {
		t.Fatalf("invalid second row was not rejected before persistence: count=%d, err=%v", count, err)
	}
}

func geneMetricRow() map[string]string {
	return map[string]string{
		"Col4": "DEL", "Col8": "3", "Col9": "-0.9", "Col10": "-1.0", "Col11": "0.1", "Col12": "1.07",
		"Col13": "30", "Col14": "0.8", "Col15": "6", "Col16": "2", "Col17": "0.003", "Col18": "HIGH", "Col19": "0.001",
	}
}

func TestCNVGeneMetricsMatchGeneOutputContract(t *testing.T) {
	var result model.CNVExon
	if err := applyCNVGeneMetrics(&result, geneMetricRow()); err != nil {
		t.Fatal(err)
	}
	if *result.CopyNumber != 1.07 || result.CopyRatio != nil || *result.Log2Median != -1 || *result.Log2Std != 0.1 || *result.Weight != 0.8 {
		t.Fatalf("CN, ratio, median, standard deviation or weight mapped to the wrong source: %+v", result)
	}
	if result.BinCount != 6 || result.SegmentCount != 2 || *result.CoverageRatio != 0.003 || result.ConfidenceLabel != "HIGH" || *result.PValue != 0.001 {
		t.Fatalf("counts, coverage, confidence or p-value lost: %+v", result)
	}
	if result.DepthRatio != nil || result.Quality != nil || result.Ratio2 != nil || result.FloatFlag != nil || result.Impact != "" {
		t.Fatal("current columns were reinterpreted as unrelated legacy fields")
	}
}

func TestCNVGeneMetricsRejectUnknownLayoutAndInvalidNumbers(t *testing.T) {
	for _, invalid := range []struct{ key, value string }{
		{"Col18", "0.5"}, {"Col19", "HIGH"}, {"Col9", "NaN"}, {"Col12", "+Inf"},
		{"Col15", "0"}, {"Col16", "1.5"}, {"Col17", "1.1"}, {"Col19", "-0.01"},
		{"Col4", "unknown"}, {"Col14", "-1"},
	} {
		t.Run(invalid.key+"="+invalid.value, func(t *testing.T) {
			row := geneMetricRow()
			row[invalid.key] = invalid.value
			if err := applyCNVGeneMetrics(&model.CNVExon{}, row); err == nil {
				t.Fatal("malformed gene metrics were accepted")
			}
		})
	}
}

func TestCNVGeneMetricsAcceptCompatibleTypeAliases(t *testing.T) {
	for input, want := range map[string]string{"Deletion": "DEL", "Amplification": "DUP", "DUP": "DUP", "Normal": "Normal"} {
		row := geneMetricRow()
		row["Col4"] = input
		var result model.CNVExon
		if err := applyCNVGeneMetrics(&result, row); err != nil || result.Type != want {
			t.Fatalf("type %s mapped to %s: %v", input, result.Type, err)
		}
	}
}
