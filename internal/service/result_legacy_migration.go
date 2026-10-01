package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm/clause"
)

type LegacyMigrationSummary struct {
	Table     string `json:"table"`
	Checked   int    `json:"checked"`
	Migrated  int    `json:"migrated"`
	Pending   int    `json:"pending"`
	Preserved int    `json:"preserved"`
}

// This command migrates only proven, unique identities in the current tenant/attempt.
// Missing provenance, truncation or duplicate source identities remain auditable pending records.
func (s *ResultService) MigrateLegacyResultAdjustments(ctx context.Context, task *model.Task, table string, execute bool) (*LegacyMigrationSummary, error) {
	summary := &LegacyMigrationSummary{Table: table}
	types := map[string]interface{}{"snv-indel": model.SNVIndel{}, "cnv-segment": model.CNVSegment{}, "cnv-exon": model.CNVExon{}, "str": model.STR{}, "mei": model.MEIVariant{}, "mt": model.MitochondrialVariant{}, "roh": model.ROHRegion{}, "upd": model.UPDRegion{}}
	sample, ok := types[table]
	if !ok {
		return nil, fmt.Errorf("invalid table")
	}
	slice := reflect.New(reflect.SliceOf(reflect.TypeOf(sample)))
	query := database.DB.WithContext(ctx).Where("task_id=? AND tenant_id=? AND (execution_attempt_id=? OR execution_attempt_id='')", task.UUID, model.TenantIDForTask(task), executionAttempt(task))
	// CNV assessments may exist even when neither review marker is set.
	if table != "cnv-segment" && table != "cnv-exon" {
		query = query.Where("reviewed=true OR reported=true")
	}
	if err := query.Find(slice.Interface()).Error; err != nil {
		return nil, err
	}
	var assessments []model.CNVAssessment
	if table == "cnv-segment" || table == "cnv-exon" {
		if err := database.DB.WithContext(ctx).Where("task_id=? AND tenant_id=? AND execution_attempt_id=? AND variant_type=?", task.UUID, model.TenantIDForTask(task), executionAttempt(task), table).Find(&assessments).Error; err != nil {
			return nil, err
		}
	}
	assessmentByID := map[string]model.CNVAssessment{}
	for _, a := range assessments {
		assessmentByID[a.VariantID] = a
	}
	fields := legacyIdentityFields(table)
	rows := slice.Elem()
	for i := 0; i < rows.Len(); i++ {
		value := rows.Index(i)
		raw, _ := json.Marshal(value.Interface())
		var old map[string]interface{}
		_ = json.Unmarshal(raw, &old)
		review, _ := old["reviewStatus"].(map[string]interface{})
		reviewed, _ := review["reviewed"].(bool)
		reported, _ := review["reported"].(bool)
		legacyID, _ := old["id"].(string)
		assessment, hasAssessment := assessmentByID[legacyID]
		if !reviewed && !reported && !hasAssessment {
			continue
		}
		summary.Checked++
		mapping := model.ResultLegacyRowMapping{TenantID: model.TenantIDForTask(task), TaskUUID: task.UUID, ExecutionAttemptID: executionAttempt(task), Table: table, LegacyID: legacyID, Status: "pending", Reason: "identity not proven", UpdatedAt: time.Now().UTC()}
		var prior model.ResultLegacyRowMapping
		if database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND legacy_id=?", mapping.TenantID, mapping.TaskUUID, mapping.ExecutionAttemptID, table, legacyID).First(&prior).Error == nil && prior.Status == "migrated" {
			summary.Preserved++
			continue
		}
		provenance := value.FieldByName("ResultProvenance").Interface().(model.ResultProvenance)
		if provenance.ExecutionAttemptID != executionAttempt(task) {
			mapping.Reason = "missing exact execution provenance"
		} else {
			// Use two selective fields, then compare the entire identity. Never choose the first match.
			preview, err := s.QueryParquetTable(ctx, task, table, model.ParquetQueryRequest{Limit: 1})
			if err != nil {
				return nil, err
			}
			filters := []model.ParquetFilter{}
			for _, field := range fields[:2] {
				column := legacyRawIdentityColumn(field, preview.Columns)
				if column == "" {
					break
				}
				filters = append(filters, model.ParquetFilter{Column: column, Operator: "equals", Value: legacyIdentityValue(old[field])})
			}
			if len(filters) == 2 {
				matches, err := s.QueryParquetTable(ctx, task, table, model.ParquetQueryRequest{Limit: 200, Filters: filters})
				if err != nil {
					return nil, err
				}
				identities := []map[string]interface{}{}
				if matches.Total <= 200 {
					for _, row := range matches.Items {
						if sameLegacyIdentity(old, row, fields) {
							identities = append(identities, row)
						}
					}
				}
				if len(identities) == 1 {
					row := identities[0]
					mapping.RowID = fmt.Sprint(row["id"])
					mapping.DatasetVersion = matches.Version
					adjustment, err := s.GetParquetRowAdjustment(ctx, task, table, mapping.RowID)
					if err != nil {
						return nil, err
					}
					if adjustment.Version > 0 {
						mapping.Status = "preserved"
						mapping.Reason = "current adjustment already exists"
						summary.Preserved++
					} else {
						payload := map[string]interface{}{"reviewed": reviewed, "reported": reported, "legacyReview": review}
						// legacyReview is audit-only; retain the original actor/timestamps in migration reason/history.
						delete(payload, "legacyReview")
						if hasAssessment {
							var cnv map[string]interface{}
							if json.Unmarshal([]byte(assessment.PayloadJSON), &cnv) != nil {
								return nil, fmt.Errorf("invalid legacy assessment")
							}
							cnv["cnvId"] = mapping.RowID
							payload["cnvAssessment"] = cnv
						}
						if execute {
							reasonBytes, _ := json.Marshal(review)
							_, _, err = s.SaveParquetRowAdjustment(ctx, task, table, mapping.RowID, "legacy-migration", model.ResultRowAdjustmentRequest{AttemptID: executionAttempt(task), DatasetVersion: matches.Version, ExpectedVersion: 0, Adjustments: payload, Reason: "Proven legacy row " + legacyID + "; original review metadata: " + string(reasonBytes)})
							if err != nil {
								return nil, err
							}
						}
						mapping.Status = "migrated"
						mapping.Reason = "unique full identity and exact tenant/attempt"
						summary.Migrated++
					}
				} else {
					mapping.Reason = "source identity missing, ambiguous or exceeds candidate limit"
				}
			} else {
				mapping.Reason = "required identity columns missing"
			}
		}
		if mapping.Status == "pending" {
			summary.Pending++
		}
		if execute {
			if err := database.DB.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&mapping).Error; err != nil {
				return nil, err
			}
		}
	}
	return summary, nil
}

func legacyIdentityFields(table string) []string {
	switch table {
	case "snv-indel":
		return []string{"chromosome", "position", "ref", "alt", "gene", "transcript"}
	case "mt":
		return []string{"chromosome", "position", "ref", "alt", "gene"}
	case "str":
		return []string{"chromosome", "position", "gene", "repeatUnit"}
	case "mei":
		return []string{"chromosome", "position", "gene", "teType", "teFamily"}
	case "cnv-segment":
		return []string{"chromosome", "startPosition", "endPosition", "type"}
	case "cnv-exon":
		return []string{"chromosome", "startPosition", "endPosition", "type", "gene", "transcript"}
	case "roh":
		return []string{"chr", "begin", "end"}
	default:
		return []string{"chromosome", "startPosition", "endPosition", "type"}
	}
}
func legacyRawIdentityColumn(field string, columns []string) string {
	aliases := map[string][]string{"chromosome": {"Chromosome", "Chr", "Col1"}, "chr": {"Chr", "Chromosome"}, "position": {"Position"}, "startPosition": {"Start", "Start_Position", "Begin", "Col2"}, "begin": {"Begin", "Start"}}
	for _, candidate := range aliases[field] {
		for _, column := range columns {
			if candidate == column {
				return column
			}
		}
	}
	return ""
}
func sameLegacyIdentity(old, row map[string]interface{}, fields []string) bool {
	for _, field := range fields {
		current := row[field]
		if current == nil {
			if field == "chr" {
				current = row["chromosome"]
			}
			if field == "begin" {
				current = row["startPosition"]
			}
			if field == "end" {
				current = row["endPosition"]
			}
		}
		if old[field] == nil || current == nil || strings.TrimSpace(legacyIdentityValue(old[field])) != legacyIdentityValue(current) {
			return false
		}
	}
	return true
}

func legacyIdentityValue(value interface{}) string {
	if number, ok := value.(float64); ok {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
