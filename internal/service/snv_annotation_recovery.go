package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type snvAnnotationUpdate struct {
	ID               string `gorm:"primaryKey"`
	AnnotationValues string `gorm:"type:jsonb"`
	PangolinAN       string
	EVOScoreAN       string
}

func snvSourceKey(chromosome, position, ref, alt, gene, transcript string) string {
	key, _ := json.Marshal([]string{chromosome, position, ref, alt, gene, transcript})
	return string(key)
}

func buildSNVAnnotationUpdates(rows []map[string]string, variants []model.SNVIndel) ([]snvAnnotationUpdate, error) {
	if len(rows) == 0 || len(rows) != len(variants) {
		return nil, fmt.Errorf("archive and imported SNV row counts differ")
	}
	byKey := make(map[string]string, len(variants))
	for _, variant := range variants {
		key := snvSourceKey(variant.Chromosome, fmt.Sprint(variant.Position), variant.Ref, variant.Alt, variant.Gene, variant.Transcript)
		if _, exists := byKey[key]; exists {
			return nil, fmt.Errorf("ambiguous imported SNV identity")
		}
		byKey[key] = variant.ID
	}
	updates := make([]snvAnnotationUpdate, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		key := snvSourceKey(row["Chromosome"], fmt.Sprint(parseInt64(row["Position"])), row["Ref"], row["Alt"], row["Gene"], row["Transcript"])
		id, exists := byKey[key]
		if !exists || seen[id] {
			return nil, fmt.Errorf("archive SNV identity does not match imported results")
		}
		seen[id] = true
		values, _ := json.Marshal(snvAnnotationValues(row))
		updates = append(updates, snvAnnotationUpdate{ID: id, AnnotationValues: string(values), PangolinAN: annotationText(row["Pangolin_AN"]), EVOScoreAN: annotationText(row["EVOScore_AN"])})
	}
	return updates, nil
}

// Operator-only repair for annotations lost by old importers. It never deletes
// results or changes review/ACMG decisions, task lifecycle, billing or manifests.
func (s *TaskService) RecoverArchivedSNVAnnotations(ctx context.Context, taskUUID, attempt string, execute bool) (map[string]interface{}, error) {
	check, err := s.InspectArchivedTaskResults(taskUUID, attempt)
	if err != nil {
		return nil, err
	}
	if check.ImportStatus != model.ResultImportStatusSuccess {
		return nil, fmt.Errorf("annotation recovery requires a successful result import")
	}
	files := NewImporter(s.cfg).findResultFiles(check.ArchiveDirectory)
	var source string
	for _, file := range files {
		name := strings.ToLower(filepath.Base(file))
		if strings.Contains(name, "snv_indel") || strings.Contains(name, "snv.indel") {
			if source != "" {
				return nil, fmt.Errorf("multiple SNV reports are ambiguous")
			}
			source = file
		}
	}
	if source == "" {
		return nil, fmt.Errorf("SNV source report not found")
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, fmt.Errorf("SNV source unavailable")
	}
	base, err := filepath.EvalSymlinks(check.ArchiveDirectory)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("SNV source escapes archive")
	}
	headers, rows, err := readTSV(resolved)
	if err != nil {
		return nil, fmt.Errorf("cannot read SNV source")
	}
	required := map[string]bool{"Pangolin_AN": false, "EVOScore_AN": false, "AlphaMissense_AM": false, "GnomAD_AF": false}
	for _, header := range headers {
		if _, ok := required[header]; ok {
			required[header] = true
		}
	}
	for _, present := range required {
		if !present {
			return nil, fmt.Errorf("SNV report lacks required annotation columns")
		}
	}
	result := map[string]interface{}{"mode": "dry_run", "task_uuid": taskUUID, "attempt_id": attempt, "rows": len(rows), "updated": int64(0)}
	err = database.GetDB().WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid = ?", taskUUID).First(&task).Error; err != nil {
			return err
		}
		if task.ExecutionAttemptID != attempt || task.Status != model.TaskStatusCompleted || task.ResultImportStatus != model.ResultImportStatusSuccess {
			return fmt.Errorf("task execution or import state changed")
		}
		var variants []model.SNVIndel
		scope := tx.Where("task_id = ? AND tenant_id = ? AND execution_attempt_id = ?", taskUUID, model.TenantIDForTask(&task), attempt)
		if err := scope.Select("id, chromosome, position, ref, alt, gene, transcript").Find(&variants).Error; err != nil {
			return err
		}
		updates, err := buildSNVAnnotationUpdates(rows, variants)
		if err != nil {
			return err
		}
		if !execute {
			return nil
		}
		if err := tx.Exec(`CREATE TEMP TABLE snv_annotation_updates (id text PRIMARY KEY, annotation_values jsonb, pangolin_an text, evo_score_an text) ON COMMIT DROP`).Error; err != nil {
			return err
		}
		if err := tx.Table("snv_annotation_updates").CreateInBatches(updates, 500).Error; err != nil {
			return err
		}
		updated := tx.Exec(`UPDATE result_snv_indels r SET annotation_values=u.annotation_values,pangolin_an=u.pangolin_an,evo_score_an=u.evo_score_an FROM snv_annotation_updates u WHERE r.id=u.id AND r.task_id=? AND r.tenant_id=? AND r.execution_attempt_id=? AND (r.annotation_values IS DISTINCT FROM u.annotation_values OR r.pangolin_an IS DISTINCT FROM u.pangolin_an OR r.evo_score_an IS DISTINCT FROM u.evo_score_an)`, taskUUID, model.TenantIDForTask(&task), attempt)
		if updated.Error != nil {
			return updated.Error
		}
		result["mode"] = "executed"
		result["updated"] = updated.RowsAffected
		if updated.RowsAffected == 0 {
			return nil
		}
		hash := sha256.New()
		_, _ = io.WriteString(hash, task.ResultImportFingerprint+":snv-source-annotations-v1:")
		file, err := os.Open(resolved)
		if err != nil {
			return err
		}
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil {
			return err
		}
		fingerprint := hex.EncodeToString(hash.Sum(nil))
		now := time.Now().UTC()
		counts, _ := json.Marshal(map[string]int64{"snv_annotations": updated.RowsAffected})
		batch := model.ResultImportBatch{TaskUUID: taskUUID, TenantID: model.TenantIDForTask(&task), ExecutionAttemptID: attempt, Source: "annotation_recovery", Status: model.ResultImportBatchStatusSuccess, Fingerprint: fingerprint, ArchiveBase: check.ArchiveDirectory, ObjectKeysJSON: "[]", CountsJSON: string(counts), StartedAt: now, FinishedAt: &now}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		return tx.Model(&model.Task{}).Where("uuid = ? AND execution_attempt_id = ?", taskUUID, attempt).Updates(map[string]interface{}{"result_import_fingerprint": fingerprint, "version": gorm.Expr("version + 1")}).Error
	})
	return result, err
}
