package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
)

// Supplement older QC projections only from the authorized task's active archive.
// This never reimports variants, updates the database, or falls back to an old attempt.
func (s *ResultService) enrichArchivedQC(task *model.Task, rows []model.QCResult) []model.QCResult {
	needed := false
	for _, row := range rows {
		var available map[string]bool
		_ = json.Unmarshal([]byte(row.MetricAvailability), &available)
		if !available["beforeTotalReads"] || !available["coverageGt02Avg"] || !available["sryCount"] {
			needed = true
		}
	}
	if !needed || s.cfg == nil || s.cfg.Task.ArchiveDir == "" {
		return rows
	}
	dir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
		return rows
	}
	data, err := (&Archiver{cfg: s.cfg}).readArchiveJSONFile(dir, "outputs.resolved.json")
	if err != nil {
		return rows
	}
	var manifest map[string]interface{}
	if json.Unmarshal(data, &manifest) != nil {
		return rows
	}
	type document struct {
		data map[string]interface{}
		role string
	}
	var documents []document
	for _, value := range manifest {
		summary, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		roles := resultMemberRoles(summary["members"])
		switch qc := summary["qc_result"].(type) {
		case map[string]interface{}:
			documents = append(documents, document{qc, memberRoleAt(roles, 0, false)})
		case []interface{}:
			for i, item := range qc {
				if qc, ok := item.(map[string]interface{}); ok {
					documents = append(documents, document{qc, memberRoleAt(roles, i, true)})
				}
			}
		}
	}
	for i := range rows {
		row := &rows[i]
		available := map[string]bool{}
		_ = json.Unmarshal([]byte(row.MetricAvailability), &available)
		if available == nil {
			available = map[string]bool{}
		}
		var matched []document
		for _, doc := range documents {
			sid, _ := doc.data["sample_id"].(string)
			if (row.SampleID != "" && sid == row.SampleID && doc.role == row.MemberRole) ||
				(len(rows) == 1 && len(documents) == 1 && (sid == row.SampleID || row.SampleID == "")) {
				matched = append(matched, doc)
			}
		}
		if len(matched) != 1 {
			continue
		}
		qc := matched[0].data
		if fastp, ok := qc["fastp"].(map[string]interface{}); ok && !available["beforeTotalReads"] {
			if before, ok := fastp["before_filtering"].(map[string]interface{}); ok {
				if value, ok := before["total_reads"].(float64); ok && value >= 0 && value == float64(int64(value)) {
					row.BeforeTotalReads = int64(value)
					available["beforeTotalReads"] = true
				}
			}
		}
		if xd, ok := qc["xamdst"].(map[string]interface{}); ok && !available["coverageGt02Avg"] {
			if value, ok := xd["coverage_gt_0_2_avg"].(float64); ok && value >= 0 && value <= 100 {
				row.CoverageGt02Avg = value
				available["coverageGt02Avg"] = true
			}
		}
		if sry, ok := qc["sry"].(map[string]interface{}); ok && !available["sryCount"] {
			if value, ok := sry["sry_count"].(float64); ok && value >= 0 && value == float64(int(value)) {
				row.SryCount = int(value)
				available["sryCount"] = true
			}
		}
		encoded, _ := json.Marshal(available)
		row.MetricAvailability = string(encoded)
	}
	return rows
}

func normalizeQCGender(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "male", "m", "男":
		return "male"
	case "female", "f", "女":
		return "female"
	default:
		return "unknown"
	}
}

func (s *ResultService) addQCGenderComparison(ctx context.Context, task *model.Task, qc []model.QCMemberSummary) error {
	if len(qc) == 0 {
		return nil
	}
	declared := map[string]string{}
	// Never infer the declared sex from a family role. Read the explicitly linked sample.
	findGender := func(sampleID string) (string, error) {
		if sampleID == "" {
			return "unknown", nil
		}
		var samples []model.Sample
		query := database.DB.WithContext(ctx).Select("gender").Where("uuid = ?", sampleID)
		if task.ExternalOrgID != "" {
			query = query.Where("external_org_id = ?", task.ExternalOrgID)
		} else {
			query = query.Where("created_by = ?", task.CreatedBy)
		}
		if err := query.Limit(1).Find(&samples).Error; err != nil {
			return "unknown", err
		}
		if len(samples) == 0 {
			return "unknown", nil
		}
		return normalizeQCGender(string(samples[0].Gender)), nil
	}
	gender, err := findGender(task.SampleID)
	if err != nil {
		return err
	}
	declared["proband"], declared["single"], declared["patient"] = gender, gender, gender
	if task.PedigreeID != "" {
		var pedigrees []model.Pedigree
		if err := database.DB.WithContext(ctx).Where("id = ? AND created_by = ?", task.PedigreeID, task.CreatedBy).Limit(1).Find(&pedigrees).Error; err != nil {
			return err
		}
		if len(pedigrees) == 1 {
			var members []model.PedigreeMember
			if err := database.DB.WithContext(ctx).Where("pedigree_id = ?", task.PedigreeID).Find(&members).Error; err != nil {
				return err
			}
			byID := map[string]model.PedigreeMember{}
			for _, member := range members {
				byID[member.ID] = member
			}
			proband, exists := byID[pedigrees[0].ProbandMemberID]
			if exists {
				for role, id := range map[string]string{"proband": proband.ID, "father": proband.FatherID, "mother": proband.MotherID} {
					if member, exists := byID[id]; exists {
						value, err := findGender(member.SampleID)
						if err != nil {
							return err
						}
						declared[role] = value
					}
				}
			}
		}
	}
	var inputs map[string]interface{}
	_ = json.Unmarshal([]byte(task.InputJSON), &inputs)
	var cutoff *float64
	for _, key := range []string{"SingleWES.sry_sex_cutoff", "TrioWES.sry_sex_cutoff"} {
		if value, ok := inputs[key].(float64); ok && value >= 0 {
			copy := value
			cutoff = &copy
		}
	}
	for i := range qc {
		member := &qc[i]
		member.DeclaredGender = normalizeQCGender(declared[strings.ToLower(member.MemberRole)])
		member.PredictedGender = normalizeQCGender(member.PredictedGender)
		member.SRYCutoff = cutoff
		var count *float64
		for _, metric := range member.Metrics {
			if metric.Key == "sryCount" {
				count = metric.Value
			}
		}
		if count == nil {
			member.PredictedGender = "unknown"
		} else if cutoff != nil {
			member.PredictedGender = "female"
			if *count > *cutoff {
				member.PredictedGender = "male"
			}
		}
		member.GenderComparison = "unknown"
		if member.DeclaredGender != "unknown" && member.PredictedGender != "unknown" {
			member.GenderComparison = "match"
			if member.DeclaredGender != member.PredictedGender {
				member.GenderComparison = "mismatch"
			}
		}
	}
	return nil
}
