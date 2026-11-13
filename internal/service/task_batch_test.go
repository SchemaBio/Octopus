package service

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func taskBatchWorkbook(t *testing.T, sheetName string, rows [][]interface{}) *bytes.Reader {
	t.Helper()
	book := excelize.NewFile()
	defaultSheet := book.GetSheetName(0)
	if err := book.SetSheetName(defaultSheet, sheetName); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}
	for rowIndex, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, rowIndex+1)
		if err := book.SetSheetRow(sheetName, cell, &row); err != nil {
			t.Fatalf("set row: %v", err)
		}
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	if err := book.Close(); err != nil {
		t.Fatalf("close workbook: %v", err)
	}
	return bytes.NewReader(buffer.Bytes())
}

func TestParseTaskBatchWorkbook(t *testing.T) {
	reader := taskBatchWorkbook(t, taskBatchSheetName, [][]interface{}{
		{"样本ID", "家系ID", "流程ID", "任务备注"},
		{"TEST-001", "", "builtin-wes-single", "首批测试"},
		{"", "pedigree-1", "builtin-wes-family", ""},
		{"", "", "", ""},
	})
	rows, err := ParseTaskBatchWorkbook(reader)
	if err != nil {
		t.Fatalf("ParseTaskBatchWorkbook returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].RowNumber != 2 || rows[0].SampleIdentifier != "TEST-001" || rows[0].Remark != "首批测试" {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].RowNumber != 3 || rows[1].PedigreeID != "pedigree-1" {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
}

func TestParseTrioBatchUsesProbandWithoutPedigreeColumn(t *testing.T) {
	reader := taskBatchWorkbook(t, taskBatchSheetName, [][]interface{}{{"样本ID", "流程ID", "任务备注"}, {"PROBAND-001", "builtin-wes-family", ""}})
	rows, err := ParseTaskBatchWorkbook(reader)
	if err != nil || len(rows) != 1 || rows[0].SampleIdentifier != "PROBAND-001" || rows[0].PedigreeID != "" {
		t.Fatalf("new Trio template rejected: %+v %v", rows, err)
	}
}

func TestParseTaskBatchWorkbookIgnoresLegacyCNVAndSVColumns(t *testing.T) {
	reader := taskBatchWorkbook(t, taskBatchSheetName, [][]interface{}{
		{"样本ID", "家系ID", "流程ID", "任务备注", "启用CNV", "启用SV"},
		{"TEST-001", "", "builtin-wes-single", "", "随便", "TRUE"},
	})
	rows, err := ParseTaskBatchWorkbook(reader)
	if err != nil {
		t.Fatalf("ParseTaskBatchWorkbook returned error: %v", err)
	}
	if len(rows) != 1 || len(rows[0].ParseErrors) != 0 || rows[0].SampleIdentifier != "TEST-001" {
		t.Fatalf("legacy CNV/SV columns should be ignored, got %+v", rows)
	}
}

func TestParseTaskBatchWorkbookRequiresTemplateSheet(t *testing.T) {
	reader := taskBatchWorkbook(t, "Sheet1", [][]interface{}{{"样本ID"}})
	if _, err := ParseTaskBatchWorkbook(reader); err == nil || !strings.Contains(err.Error(), taskBatchSheetName) {
		t.Fatalf("expected missing template sheet error, got %v", err)
	}
}

func TestParseTaskBatchWorkbookEnforcesRowLimit(t *testing.T) {
	rows := [][]interface{}{{"样本ID", "家系ID", "流程ID", "任务备注"}}
	for index := 0; index < maxTaskBatchRows+1; index++ {
		rows = append(rows, []interface{}{fmt.Sprintf("sample-%d", index), "", "builtin-wes-single", ""})
	}
	reader := taskBatchWorkbook(t, taskBatchSheetName, rows)
	if _, err := ParseTaskBatchWorkbook(reader); err == nil || !strings.Contains(err.Error(), "最多") {
		t.Fatalf("expected row limit error, got %v", err)
	}
}
