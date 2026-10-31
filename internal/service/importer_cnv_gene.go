package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
)

// CNVAnno retains the 19 original gene columns as Col4..Col19. Validate this
// layout before writing any rows; never reinterpret absolute CN as a ratio.
func applyCNVGeneMetrics(result *model.CNVExon, row map[string]string) error {
	confidence := row["Col18"]
	if confidence != "HIGH" && confidence != "MEDIUM" && confidence != "LOW" {
		return fmt.Errorf("unsupported CNV gene TSV layout: expected confidence in Col18")
	}
	for _, field := range []struct {
		column string
		target **float64
	}{
		{"Col9", &result.Log2Ratio}, {"Col10", &result.Log2Median},
		{"Col11", &result.Log2Std}, {"Col12", &result.CopyNumber},
		{"Col13", &result.Depth}, {"Col14", &result.Weight},
		{"Col17", &result.CoverageRatio}, {"Col19", &result.PValue},
	} {
		value, err := strconv.ParseFloat(row[field.column], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("invalid CNV gene %s: expected a finite number", field.column)
		}
		*field.target = &value
	}
	for _, field := range []struct {
		column string
		target *int
	}{
		{"Col8", &result.ExonCount}, {"Col15", &result.BinCount}, {"Col16", &result.SegmentCount},
	} {
		value, err := strconv.Atoi(row[field.column])
		if err != nil || value < 1 {
			return fmt.Errorf("invalid CNV gene %s: expected a positive count", field.column)
		}
		*field.target = value
	}
	if *result.Log2Std < 0 || *result.CopyNumber < 0 || *result.Depth < 0 || *result.Weight < 0 ||
		*result.CoverageRatio < 0 || *result.CoverageRatio > 1 || *result.PValue < 0 || *result.PValue > 1 {
		return fmt.Errorf("invalid CNV gene numeric range")
	}
	result.ConfidenceLabel = confidence
	// Annotation adapters restore these original type labels. Accept the long
	// forms as well for files produced by other compatible annotator versions.
	switch strings.ToUpper(row["Col4"]) {
	case "DEL", "DELETION", "LOSS":
		result.Type = "DEL"
	case "DUP", "DUPLICATION", "AMPLIFICATION", "GAIN":
		result.Type = "DUP"
	case "NORMAL":
		result.Type = "Normal"
	default:
		return fmt.Errorf("unsupported CNV gene type")
	}
	return nil
}
