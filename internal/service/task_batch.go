package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/xuri/excelize/v2"
)

const (
	maxTaskBatchRows     = 100
	taskBatchSheetName   = "批量任务"
	taskBatchMaxUnzipped = 16 << 20
)

var taskBatchHeaders = map[string]string{
	"样本id":             "sample",
	"sampleid":         "sample",
	"sampleidentifier": "sample",
	"家系id":             "pedigree",
	"pedigreeid":       "pedigree",
	"流程id":             "pipeline",
	"pipelineid":       "pipeline",
	"任务备注":             "remark",
	"remark":           "remark",
	"启用cnv":            "enable_cnv",
	"enablecnv":        "enable_cnv",
	"启用sv":             "enable_sv",
	"enablesv":         "enable_sv",
}

func normalizeTaskBatchHeader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer(" ", "", "_", "", "-", "", "\u3000", "").Replace(value)
}

func parseTaskBatchBool(value string, defaultValue bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return defaultValue, nil
	case "是", "true", "1", "yes", "y":
		return true, nil
	case "否", "false", "0", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("必须填写 是/否、TRUE/FALSE 或 1/0")
	}
}

// ParseTaskBatchWorkbook reads the first worksheet named 批量任务 and returns
// normalized rows. Workbook formulas and formatting are deliberately ignored.
func ParseTaskBatchWorkbook(reader io.Reader) ([]model.TaskBatchInputRow, error) {
	book, err := excelize.OpenReader(reader, excelize.Options{
		RawCellValue:      true,
		UnzipSizeLimit:    taskBatchMaxUnzipped,
		UnzipXMLSizeLimit: taskBatchMaxUnzipped / 2,
	})
	if err != nil {
		return nil, fmt.Errorf("无法读取 XLSX 文件: %w", err)
	}
	defer book.Close()

	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("XLSX 文件不包含工作表")
	}
	sheetName := taskBatchSheetName
	if _, err := book.GetSheetIndex(sheetName); err != nil {
		return nil, fmt.Errorf("缺少名为 %q 的工作表，请使用平台模板", taskBatchSheetName)
	}
	rows, err := book.GetRows(sheetName, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("读取工作表失败: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("批量任务工作表为空")
	}

	columns := make(map[string]int)
	for index, header := range rows[0] {
		if key, ok := taskBatchHeaders[normalizeTaskBatchHeader(header)]; ok {
			columns[key] = index
		}
	}
	for _, required := range []string{"sample", "pedigree", "pipeline", "remark", "enable_cnv", "enable_sv"} {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("缺少模板列，请重新下载平台模板")
		}
	}

	cell := func(row []string, key string) string {
		index := columns[key]
		if index >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[index])
	}
	parsed := make([]model.TaskBatchInputRow, 0, len(rows)-1)
	for index, row := range rows[1:] {
		sampleIdentifier := cell(row, "sample")
		pedigreeID := cell(row, "pedigree")
		pipelineID := cell(row, "pipeline")
		remark := cell(row, "remark")
		cnvValue := cell(row, "enable_cnv")
		svValue := cell(row, "enable_sv")
		if sampleIdentifier == "" && pedigreeID == "" && pipelineID == "" && remark == "" && cnvValue == "" && svValue == "" {
			continue
		}
		if len(parsed) >= maxTaskBatchRows {
			return nil, fmt.Errorf("每次最多导入 %d 个任务", maxTaskBatchRows)
		}
		enableCNV, cnvErr := parseTaskBatchBool(cnvValue, true)
		enableSV, svErr := parseTaskBatchBool(svValue, false)
		parsedRow := model.TaskBatchInputRow{
			RowNumber:        index + 2,
			SampleIdentifier: sampleIdentifier,
			PedigreeID:       pedigreeID,
			PipelineID:       pipelineID,
			Remark:           remark,
			EnableCNV:        enableCNV,
			EnableSV:         enableSV,
			ParseErrors:      make([]string, 0, 2),
		}
		if cnvErr != nil {
			parsedRow.ParseErrors = append(parsedRow.ParseErrors, fmt.Sprintf("启用CNV值 %q 无效，%v", cnvValue, cnvErr))
		}
		if svErr != nil {
			parsedRow.ParseErrors = append(parsedRow.ParseErrors, fmt.Sprintf("启用SV值 %q 无效，%v", svValue, svErr))
		}
		parsed = append(parsed, parsedRow)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("批量任务工作表没有可导入的数据行")
	}
	return parsed, nil
}

func (s *TaskService) previewTaskBatchRow(input model.TaskBatchInputRow, actor model.OverlayActor) (model.TaskBatchPreviewRow, *model.TaskCreateRequest) {
	input.SampleIdentifier = strings.TrimSpace(input.SampleIdentifier)
	input.PedigreeID = strings.TrimSpace(input.PedigreeID)
	input.PipelineID = strings.TrimSpace(input.PipelineID)
	input.Remark = strings.TrimSpace(input.Remark)
	preview := model.TaskBatchPreviewRow{TaskBatchInputRow: input, Errors: append([]string(nil), input.ParseErrors...)}
	if input.RowNumber < 2 {
		preview.Errors = append(preview.Errors, "行号无效")
	}
	if input.PipelineID == "" {
		preview.Errors = append(preview.Errors, "流程ID不能为空")
	}
	if len(input.Remark) > 1000 {
		preview.Errors = append(preview.Errors, "任务备注不能超过 1000 个字符")
	}

	req := &model.TaskCreateRequest{
		SampleID:   input.SampleIdentifier,
		PedigreeID: input.PedigreeID,
		PipelineID: input.PipelineID,
		Remark:     input.Remark,
		Inputs: map[string]interface{}{
			"enable_cnv": input.EnableCNV,
			"enable_sv":  input.EnableSV,
		},
	}
	if input.PipelineID != "" {
		if _, err := s.resolveAnalysisPipeline(req, actor); err != nil {
			preview.Errors = append(preview.Errors, err.Error())
		} else {
			preview.PipelineName = req.PipelineName
			preview.PipelineVersion = req.PipelineVersion
			preview.Template = req.Template
		}
	}

	if req.Template == "trio" {
		if input.PedigreeID == "" {
			preview.Errors = append(preview.Errors, "家系任务必须填写家系ID")
		} else if input.SampleIdentifier != "" {
			preview.Errors = append(preview.Errors, "家系任务不应填写样本ID")
		} else {
			proband, _, err := s.prepareTrioInputs(input.PedigreeID, "batch-preview", actor, req.Inputs)
			if err != nil {
				preview.Errors = append(preview.Errors, err.Error())
			} else {
				req.SampleID, req.InternalID = proband.UUID, proband.InternalID
				preview.SampleID, preview.SampleInternalID = proband.UUID, proband.InternalID
				preview.EstimatedMinutes = s.estimateTaskMinutes(proband.ID, nil)
			}
		}
	} else if req.Template != "" {
		if input.SampleIdentifier == "" {
			preview.Errors = append(preview.Errors, "单样本任务必须填写样本ID")
		} else if input.PedigreeID != "" {
			preview.Errors = append(preview.Errors, "单样本任务不应填写家系ID")
		} else {
			sample, err := s.sampleRepo.FindScopedByIdentifier(input.SampleIdentifier, actor)
			if err != nil {
				preview.Errors = append(preview.Errors, fmt.Sprintf("样本不存在或无权访问: %s", input.SampleIdentifier))
			} else {
				req.SampleID, req.InternalID = sample.UUID, sample.InternalID
				preview.SampleID, preview.SampleInternalID = sample.UUID, sample.InternalID
				preview.EstimatedMinutes = s.estimateTaskMinutes(sample.ID, nil)
			}
		}
	}
	preview.Valid = len(preview.Errors) == 0
	return preview, req
}

func (s *TaskService) PreviewTaskBatch(inputs []model.TaskBatchInputRow, actor model.OverlayActor) *model.TaskBatchPreviewResponse {
	response := &model.TaskBatchPreviewResponse{Rows: make([]model.TaskBatchPreviewRow, 0, len(inputs)), TotalRows: len(inputs)}
	seen := make(map[string]int)
	for _, input := range inputs {
		preview, _ := s.previewTaskBatchRow(input, actor)
		key := strings.ToLower(strings.Join([]string{preview.SampleIdentifier, preview.PedigreeID, preview.PipelineID}, "\x1f"))
		if firstRow, ok := seen[key]; ok && preview.Valid {
			preview.Valid = false
			preview.Errors = append(preview.Errors, fmt.Sprintf("与第 %d 行重复", firstRow))
		} else if key != "\x1f\x1f" {
			seen[key] = preview.RowNumber
		}
		if preview.Valid {
			response.ValidRows++
			response.TotalEstimatedMinutes += preview.EstimatedMinutes
		} else {
			response.InvalidRows++
		}
		response.Rows = append(response.Rows, preview)
	}
	return response
}

func (s *TaskService) CreateTaskBatch(ctx context.Context, request *model.TaskBatchCreateRequest, actor model.OverlayActor) *model.TaskBatchCreateResponse {
	response := &model.TaskBatchCreateResponse{Results: make([]model.TaskBatchCreateResult, 0, len(request.Rows))}
	if len(request.Rows) == 0 || len(request.Rows) > maxTaskBatchRows {
		response.FailedCount = len(request.Rows)
		response.Results = append(response.Results, model.TaskBatchCreateResult{Status: "failed", Error: fmt.Sprintf("每次必须提交 1-%d 个任务", maxTaskBatchRows)})
		return response
	}
	preview := s.PreviewTaskBatch(request.Rows, actor)
	if preview.InvalidRows > 0 {
		for _, row := range preview.Rows {
			result := model.TaskBatchCreateResult{RowNumber: row.RowNumber, Status: "failed", Error: strings.Join(row.Errors, "；")}
			if row.Valid {
				result.Status = "skipped"
				result.Error = "批次中存在无效行，未创建任何任务"
				response.SkippedCount++
			} else {
				response.FailedCount++
			}
			response.Results = append(response.Results, result)
		}
		return response
	}

	for index, input := range request.Rows {
		previewRow, taskRequest := s.previewTaskBatchRow(input, actor)
		task, err := s.CreateTask(ctx, taskRequest, actor)
		if err != nil {
			response.Results = append(response.Results, model.TaskBatchCreateResult{RowNumber: previewRow.RowNumber, Status: "failed", Error: err.Error()})
			response.FailedCount++
			for _, remaining := range request.Rows[index+1:] {
				response.Results = append(response.Results, model.TaskBatchCreateResult{RowNumber: remaining.RowNumber, Status: "skipped", Error: "前一行创建失败，后续任务未提交"})
				response.SkippedCount++
			}
			break
		}
		taskResponse := task.ToResponse()
		response.Results = append(response.Results, model.TaskBatchCreateResult{RowNumber: previewRow.RowNumber, Status: "created", Task: &taskResponse})
		response.CreatedCount++
	}
	return response
}

func IsTaskBatchWorkbook(filename string) bool {
	return strings.EqualFold(filepath.Ext(strings.TrimSpace(filename)), ".xlsx")
}
