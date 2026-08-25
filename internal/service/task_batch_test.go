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
		{"样本ID", "家系ID", "流程ID", "任务备注", "启用CNV", "启用SV"},
		{"TEST-001", "", "builtin-wes-single", "首批测试", "是", "否"},
		{"", "pedigree-1", "builtin-wes-family", "", "", "TRUE"},
		{"", "", "", "", "", ""},
	})
	rows, err := ParseTaskBatchWorkbook(reader)
	if err != nil {
		t.Fatalf("ParseTaskBatchWorkbook returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].RowNumber != 2 || rows[0].SampleIdentifier != "TEST-001" || !rows[0].EnableCNV || rows[0].EnableSV {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].RowNumber != 3 || rows[1].PedigreeID != "pedigree-1" || !rows[1].EnableCNV || !rows[1].EnableSV {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
}

func TestParseTaskBatchWorkbookReportsInvalidBoolean(t *testing.T) {
	reader := taskBatchWorkbook(t, taskBatchSheetName, [][]interface{}{
		{"样本ID", "家系ID", "流程ID", "任务备注", "启用CNV", "启用SV"},
		{"TEST-001", "", "builtin-wes-single", "", "随便", "否"},
	})
	rows, err := ParseTaskBatchWorkbook(reader)
	if err != nil {
		t.Fatalf("ParseTaskBatchWorkbook returned error: %v", err)
	}
	if len(rows) != 1 || len(rows[0].ParseErrors) != 1 || !strings.Contains(rows[0].ParseErrors[0], "启用CNV") {
		t.Fatalf("expected one CNV parse error, got %+v", rows)
	}
}

func TestParseTaskBatchWorkbookRequiresTemplateSheet(t *testing.T) {
	reader := taskBatchWorkbook(t, "Sheet1", [][]interface{}{{"样本ID"}})
	if _, err := ParseTaskBatchWorkbook(reader); err == nil || !strings.Contains(err.Error(), taskBatchSheetName) {
		t.Fatalf("expected missing template sheet error, got %v", err)
	}
}

func TestParseTaskBatchWorkbookEnforcesRowLimit(t *testing.T) {
	rows := [][]interface{}{{"样本ID", "家系ID", "流程ID", "任务备注", "启用CNV", "启用SV"}}
	for index := 0; index < maxTaskBatchRows+1; index++ {
		rows = append(rows, []interface{}{fmt.Sprintf("sample-%d", index), "", "builtin-wes-single", "", "是", "否"})
	}
	reader := taskBatchWorkbook(t, taskBatchSheetName, rows)
	if _, err := ParseTaskBatchWorkbook(reader); err == nil || !strings.Contains(err.Error(), "最多") {
		t.Fatalf("expected row limit error, got %v", err)
	}
}
