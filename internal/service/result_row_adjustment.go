package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAdjustmentConflict = errors.New("RESULT_ADJUSTMENT_VERSION_CONFLICT")

type ACMGEvidenceEntry struct {
	Code     string `json:"code"`
	Strength string `json:"strength"`
	Source   string `json:"source,omitempty"`
	Note     string `json:"note,omitempty"`
}

type ACMGAssessmentResult struct {
	Profile        string              `json:"profile"`
	Score          int                 `json:"score"`
	State          string              `json:"state"`
	Classification string              `json:"classification,omitempty"`
	Evidence       []ACMGEvidenceEntry `json:"evidence"`
	Pending        []string            `json:"pending"`
}

func CalculateSNVACMG(evidence []ACMGEvidenceEntry, manualOverride string) (ACMGAssessmentResult, error) {
	result := ACMGAssessmentResult{Profile: "acmg-snv-points-v1", State: "insufficient_evidence", Evidence: append([]ACMGEvidenceEntry(nil), evidence...), Pending: []string{"疾病机制、病例/家系及实验室证据需结合具体疾病和专家组规范评估"}}
	seen := map[string]bool{}
	positiveComputational, negativeComputational := false, false
	standaloneBenign := false
	for _, item := range evidence {
		code := strings.ToUpper(strings.TrimSpace(item.Code))
		strength := strings.ToLower(strings.TrimSpace(item.Strength))
		if !validACMGCriterion(code) {
			return result, fmt.Errorf("unsupported ACMG evidence code")
		}
		if code == "" || seen[code] {
			return result, fmt.Errorf("ACMG evidence codes must be unique")
		}
		seen[code] = true
		if code == "BA1" {
			if strength != "standalone" {
				return result, fmt.Errorf("BA1 has standalone strength")
			}
			standaloneBenign = true
			continue
		}
		if code == "PP3" {
			if positiveComputational {
				return result, fmt.Errorf("PP3 may only be counted once")
			}
			positiveComputational = true
		}
		if code == "BP4" {
			if negativeComputational {
				return result, fmt.Errorf("BP4 may only be counted once")
			}
			negativeComputational = true
		}
		points := 0
		if strings.HasPrefix(code, "P") {
			switch strength {
			case "supporting":
				points = 1
			case "moderate":
				points = 2
			case "strong":
				points = 4
			case "very_strong":
				points = 8
			default:
				return result, fmt.Errorf("invalid pathogenic evidence strength")
			}
		} else if strings.HasPrefix(code, "B") {
			switch strength {
			case "supporting":
				points = -1
			case "moderate":
				points = -2
			case "strong":
				points = -4
			default:
				return result, fmt.Errorf("invalid benign evidence strength")
			}
		} else {
			return result, fmt.Errorf("unsupported ACMG evidence code")
		}
		result.Score += points
	}
	if positiveComputational && negativeComputational {
		return result, fmt.Errorf("PP3 and BP4 are mutually exclusive")
	}
	if standaloneBenign {
		if len(evidence) != 1 {
			return result, fmt.Errorf("BA1 standalone cannot be combined with other criteria")
		}
		result.Classification = "Benign"
	} else {
		switch {
		case result.Score >= 10:
			result.Classification = "Pathogenic"
		case result.Score >= 6:
			result.Classification = "Likely_Pathogenic"
		case result.Score <= -7:
			result.Classification = "Benign"
		case result.Score <= -1:
			result.Classification = "Likely_Benign"
		case len(evidence) > 0:
			result.Classification = "VUS"
		}
	}
	if result.Classification != "" {
		result.State = "classified"
	}
	if manualOverride != "" {
		switch manualOverride {
		case "Pathogenic", "Likely_Pathogenic", "VUS", "Likely_Benign", "Benign":
		default:
			return result, fmt.Errorf("invalid manual ACMG override")
		}
		result.Classification = manualOverride
		result.State = "manual_override"
	}
	return result, nil
}

func validACMGCriterion(code string) bool {
	valid := map[string]bool{
		"PVS1": true, "PS1": true, "PS2": true, "PS3": true, "PS4": true,
		"PM1": true, "PM2": true, "PM3": true, "PM4": true, "PM5": true, "PM6": true,
		"PP1": true, "PP2": true, "PP3": true, "PP4": true, "PP5": true,
		"BA1": true, "BS1": true, "BS2": true, "BS3": true, "BS4": true,
		"BP1": true, "BP2": true, "BP3": true, "BP4": true, "BP5": true, "BP6": true, "BP7": true,
	}
	return valid[code]
}

func (s *ResultService) SaveParquetRowAdjustment(ctx context.Context, task *model.Task, table, rowID, actor string, request model.ResultRowAdjustmentRequest) (*model.ResultRowAdjustment, *model.ResultRowAdjustmentEvent, error) {
	if task == nil || !validParquetTable(table) || !validResultRowID(rowID) {
		return nil, nil, fmt.Errorf("invalid result row identity")
	}
	if len(request.Reason) > 4000 {
		return nil, nil, fmt.Errorf("adjustment reason is too long")
	}
	if len(request.Adjustments) == 0 {
		return nil, nil, fmt.Errorf("at least one adjustment is required")
	}
	if _, hasEvidence := request.Adjustments["acmgEvidence"]; hasEvidence && strings.TrimSpace(request.Reason) == "" {
		return nil, nil, fmt.Errorf("ACMG evidence changes require an adjustment reason")
	}
	if _, hasOverride := request.Adjustments["acmgOverride"]; hasOverride && strings.TrimSpace(request.Reason) == "" {
		return nil, nil, fmt.Errorf("ACMG override changes require an adjustment reason")
	}
	allowed := map[string]bool{"reviewed": true, "reported": true, "interpretation": true, "acmgEvidence": true, "acmgOverride": true, "acmgOverrideReason": true, "cnvAssessment": true}
	for key, value := range request.Adjustments {
		if !allowed[key] {
			return nil, nil, fmt.Errorf("unsupported adjustment field")
		}
		switch key {
		case "reviewed", "reported":
			if _, ok := value.(bool); !ok {
				return nil, nil, fmt.Errorf("review and report values must be boolean")
			}
		case "interpretation", "acmgOverrideReason":
			if text, ok := value.(string); !ok || len(text) > 4000 {
				return nil, nil, fmt.Errorf("adjustment text is invalid")
			}
		case "acmgOverride":
			classification, ok := value.(string)
			if !ok {
				return nil, nil, fmt.Errorf("ACMG override must be a classification")
			}
			if classification != "" && classification != "Pathogenic" && classification != "Likely_Pathogenic" && classification != "VUS" && classification != "Likely_Benign" && classification != "Benign" {
				return nil, nil, fmt.Errorf("invalid ACMG override classification")
			}
		case "acmgEvidence":
			entries, ok := value.([]interface{})
			if !ok || len(entries) > 40 {
				return nil, nil, fmt.Errorf("ACMG evidence must be an array")
			}
		case "cnvAssessment":
			if table != "cnv-segment" && table != "cnv-exon" {
				return nil, nil, fmt.Errorf("CNV assessment is only valid for CNV rows")
			}
			if _, ok := value.(map[string]interface{}); !ok {
				return nil, nil, fmt.Errorf("CNV assessment must be an object")
			}
			payload, err := json.Marshal(value)
			if err != nil {
				return nil, nil, fmt.Errorf("CNV assessment is invalid")
			}
			if err := validateCNVAssessmentPayload(payload, rowID); err != nil {
				return nil, nil, err
			}
		}
	}
	if value, ok := request.Adjustments["acmgOverride"]; ok {
		if strings.TrimSpace(fmt.Sprint(value)) != "" && strings.TrimSpace(fmt.Sprint(request.Adjustments["acmgOverrideReason"])) == "" {
			return nil, nil, fmt.Errorf("manual ACMG override requires a reason")
		}
	}
	tenant, attempt := model.TenantIDForTask(task), executionAttempt(task)
	var saved model.ResultRowAdjustment
	var event model.ResultRowAdjustmentEvent
	err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.ResultRowAdjustment
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND table=? AND row_id=?", tenant, task.UUID, attempt, table, rowID).First(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		version := uint64(0)
		payload := map[string]interface{}{}
		if err == nil {
			version = current.Version
			if request.ExpectedVersion != version {
				return ErrAdjustmentConflict
			}
			if err := json.Unmarshal([]byte(current.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("stored adjustment is invalid")
			}
		} else if request.ExpectedVersion != 0 {
			return ErrAdjustmentConflict
		}
		before, _ := json.Marshal(payload)
		for key, value := range request.Adjustments {
			payload[key] = value
		}
		if raw, ok := payload["acmgEvidence"]; ok {
			data, _ := json.Marshal(raw)
			var evidence []ACMGEvidenceEntry
			if err := json.Unmarshal(data, &evidence); err != nil {
				return fmt.Errorf("ACMG evidence is invalid")
			}
			assessment, err := CalculateSNVACMG(evidence, strings.TrimSpace(fmt.Sprint(payload["acmgOverride"])))
			if err != nil {
				return err
			}
			payload["acmgClassification"] = assessment.Classification
			payload["acmgScore"] = assessment.Score
			payload["acmgProfile"] = assessment.Profile
			payload["acmgState"] = assessment.State
		}
		after, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		saved = model.ResultRowAdjustment{TenantID: tenant, TaskUUID: task.UUID, ExecutionAttemptID: attempt, Table: table, RowID: rowID, PayloadJSON: string(after), Version: version + 1, UpdatedBy: actor, UpdatedAt: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "task_uuid"}, {Name: "execution_attempt_id"}, {Name: "table"}, {Name: "row_id"}}, DoUpdates: clause.AssignmentColumns([]string{"payload_json", "version", "updated_by", "updated_at"})}).Create(&saved).Error; err != nil {
			return err
		}
		event = model.ResultRowAdjustmentEvent{ID: uuid.NewString(), TenantID: tenant, TaskUUID: task.UUID, ExecutionAttemptID: attempt, Table: table, RowID: rowID, BeforeJSON: string(before), AfterJSON: string(after), Reason: strings.TrimSpace(request.Reason), Actor: actor, CreatedAt: time.Now().UTC()}
		return tx.Create(&event).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &saved, &event, nil
}

func (s *ResultService) GetParquetRowAdjustment(ctx context.Context, task *model.Task, table, rowID string) (*model.ResultRowAdjustment, error) {
	if task == nil || !validParquetTable(table) || !validResultRowID(rowID) {
		return nil, fmt.Errorf("invalid result row identity")
	}
	var current model.ResultRowAdjustment
	err := database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND table=? AND row_id=?", model.TenantIDForTask(task), task.UUID, executionAttempt(task), table, rowID).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &model.ResultRowAdjustment{Version: 0, PayloadJSON: "{}"}, nil
	}
	if err != nil {
		return nil, err
	}
	return &current, nil
}

func (s *ResultService) ListParquetRowAdjustmentEvents(ctx context.Context, task *model.Task, table, rowID string) ([]model.ResultRowAdjustmentEvent, error) {
	if task == nil || !validParquetTable(table) || !validResultRowID(rowID) {
		return nil, fmt.Errorf("invalid result row identity")
	}
	var rows []model.ResultRowAdjustmentEvent
	err := database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND table=? AND row_id=?", model.TenantIDForTask(task), task.UUID, executionAttempt(task), table, rowID).Order("created_at DESC").Limit(200).Find(&rows).Error
	return rows, err
}

func executionAttempt(task *model.Task) string {
	if task.ExecutionAttemptID != "" {
		return task.ExecutionAttemptID
	}
	return task.UUID
}
