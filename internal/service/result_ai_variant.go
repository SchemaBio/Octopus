package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/resultengine"
	"math"
	"strconv"
	"strings"
)

// The HTTP handler checks task access before loading the immutable result row.
func (s *ResultService) AISequenceVariant(ctx context.Context, task *model.Task, supplied map[string]interface{}) (map[string]interface{}, error) {
	n, ok := supplied["rowOrdinal"].(float64)
	if !ok || n < 0 || n > 9007199254740991 || math.Trunc(n) != n {
		return nil, fmt.Errorf("variant row identity missing; refresh results")
	}
	ordinal := int64(n)
	id, _ := supplied["id"].(string)
	d, err := s.verifyBrowserRow(ctx, task, "snv-indel", id, &ordinal)
	if err != nil {
		return nil, err
	}
	if supplied["attemptId"] != executionAttempt(task) || supplied["datasetVersion"] != d.DataVersion {
		return nil, ErrAdjustmentConflict
	}
	cache, err := s.historyDatasetCache(ctx, d)
	if err != nil {
		return nil, err
	}
	page, err := NewParquetReader().ReadPage(cache, ordinal, 1)
	if err != nil || len(page.Rows) != 1 {
		return nil, ErrParquetIncomplete
	}
	raw := page.Rows[0]
	resultengine.RemapSampleColumns(raw, d.SampleColumnRoles)
	row := normalizeParquetAPIItem("snv-indel", raw)
	row["sampleGenotypes"] = aiSampleCalls(raw)
	row["id"], row["rowOrdinal"] = id, n
	row["attemptId"], row["datasetVersion"] = executionAttempt(task), d.DataVersion
	row["datasetObjectSha256"] = d.ObjectSHA256
	for source, target := range map[string]string{"clinvarRevStat": "clinvarReviewStatus", "clinvarStar": "clinvarStars", "clinvarDn": "clinvarDisease", "alphaMissenseAM": "alphaMissenseScore", "alphaMissenseAMC": "alphaMissenseClass"} {
		if value, exists := row[source]; exists {
			row[target] = value
		}
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var normalized map[string]interface{}
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func aiSampleCalls(raw map[string]interface{}) map[string]interface{} {
	calls := map[string]interface{}{}
	for _, role := range []string{"proband", "father", "mother"} {
		suffix := ""
		if role != "proband" {
			suffix = "_" + role
		}
		call := map[string]interface{}{}
		for field, key := range map[string]string{"Genotype": "genotype", "AD": "alleleDepth", "Zygosity": "zygosity"} {
			if value := raw[field+suffix]; value != nil && fmt.Sprint(value) != "." {
				call[key] = fmt.Sprint(value)
			}
		}
		for field, key := range map[string]string{"Depth": "depth", "GQ": "genotypeQuality", "VAF": "alleleFrequency"} {
			if value, err := strconv.ParseFloat(fmt.Sprint(raw[field+suffix]), 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && (field != "VAF" || value <= 1) {
				call[key] = value
			}
		}
		if _, has := call["alleleFrequency"]; has {
			call["vafSource"] = "reported"
		} else if ad, ok := call["alleleDepth"].(string); ok {
			parts := strings.Split(ad, ",")
			if len(parts) == 2 {
				ref, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
				alt, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if e1 == nil && e2 == nil && ref >= 0 && alt >= 0 && ref+alt > 0 && !math.IsInf(ref+alt, 0) {
					call["alleleFrequency"] = alt / (ref + alt)
					call["vafSource"] = "ad"
				}
			}
		}
		if len(call) > 0 {
			calls[role] = call
		}
	}
	return calls
}
