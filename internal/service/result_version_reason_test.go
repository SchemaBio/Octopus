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

func TestSVCAISaveRejectsBrowserResultAndOtherTables(t *testing.T) {
	svc := &ResultService{}
	task := &model.Task{UUID: "task"}
	row := strings.Repeat("a", 64)
	for _, tc := range []struct {
		table string
		value interface{}
		want  string
	}{
		{"snv-indel", map[string]interface{}{"classification": "Pathogenic"}, "SVC AI save accepts only a request ID"},
		{"snv-indel", "bad", "invalid SVC AI request"},
		{"cnv-exon", "11111111-1111-4111-8111-111111111111", "SVC AI result is only valid for SNP/InDel"},
	} {
		_, _, err := svc.SaveParquetRowAdjustment(context.Background(), task, tc.table, row, "actor", model.ResultRowAdjustmentRequest{Adjustments: map[string]interface{}{"svcv4AIResult": tc.value}})
		if err == nil || err.Error() != tc.want {
			t.Fatalf("unexpected validation: %v", err)
		}
	}
}
