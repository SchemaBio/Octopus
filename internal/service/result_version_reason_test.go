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
	_, _, err = svc.SaveParquetRowAdjustment(context.Background(), task, "cnv-exon", row, "actor", model.ResultRowAdjustmentRequest{Adjustments: map[string]interface{}{"cnvAssessment": map[string]interface{}{}}})
	if err == nil || err.Error() != "CNV evidence changes require an adjustment reason" {
		t.Fatalf("CNV evidence reason validation changed: %v", err)
	}
}

func TestACMGAdjustmentReasonsAreOptional(t *testing.T) {
	svc := &ResultService{}
	task := &model.Task{UUID: "task"}
	ordinal := int64(0)
	for name, adjustments := range map[string]map[string]interface{}{
		"evidence":        {"acmgEvidence": []interface{}{map[string]interface{}{"code": "PP3", "strength": "supporting"}}},
		"manual override": {"acmgOverride": "Pathogenic", "acmgOverrideReason": ""},
		"reset":           {"resetAcmg": true},
		"SVCv4":           {"svcv4Assessment": map[string]interface{}{}},
	} {
		t.Run(name, func(t *testing.T) {
			// A blank reason must pass validation and reach archive verification.
			_, _, err := svc.SaveParquetRowAdjustment(context.Background(), task, "snv-indel", strings.Repeat("a", 64), "actor", model.ResultRowAdjustmentRequest{Adjustments: adjustments, RowOrdinal: &ordinal})
			if err == nil || err.Error() != "object storage result archive is unavailable" {
				t.Fatalf("blank reason did not reach archive verification: %v", err)
			}
		})
	}
}
