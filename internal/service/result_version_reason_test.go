package service

import (
	"context"
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestVersionSelectionDoesNotRequireEvidenceReason(t *testing.T) {
	svc := &ResultService{}
	task := &model.Task{UUID: "task"}
	row := strings.Repeat("a", 64)
	// An omitted reason must reach version validation rather than the evidence gate.
	_, _, err := svc.SaveParquetRowAdjustment(context.Background(), task, "snv-indel", row, "actor", model.ResultRowAdjustmentRequest{Adjustments: map[string]interface{}{"activeAcmgVersion": "invalid"}})
	if err == nil || err.Error() != "invalid ACMG version" {
		t.Fatalf("unexpected version validation: %v", err)
	}
	for _, key := range []string{"svcv4Assessment", "acmgEvidence", "acmgOverride", "cnvAssessment"} {
		_, _, err = svc.SaveParquetRowAdjustment(context.Background(), task, "snv-indel", row, "actor", model.ResultRowAdjustmentRequest{Adjustments: map[string]interface{}{key: map[string]interface{}{}}})
		if err == nil || !strings.Contains(err.Error(), "require an adjustment reason") {
			t.Fatalf("evidence reason gate changed for %s: %v", key, err)
		}
	}
}
