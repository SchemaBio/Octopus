package service

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *TaskService) syncCNVBaselineOutput(task *model.Task, outputsJSON string) {
	if task == nil || task.Template != "baseline_fix" || task.Status != model.TaskStatusCompleted {
		return
	}
	// CVM paths belong to a disposable node. Registration uses the verified
	// archived manifest in stageCOSArchive instead of these raw workflow paths.
	if task.Executor == model.ExecutorCVM {
		return
	}
	outputPath := baselineOutputFromJSON(outputsJSON)
	if outputPath == "" {
		outputPath = findBaselineOutputFile(task.OutputDir)
	}
	if outputPath == "" {
		return
	}
	database.GetDB().Model(&model.CNVBaseline{}).
		Where("task_uuid = ?", task.UUID).
		Updates(map[string]interface{}{"output_path": outputPath})
}

func baselineOutputFromJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ""
	}
	return findBaselineOutputValue(value, "")
}

func findBaselineOutputValue(value interface{}, key string) string {
	switch current := value.(type) {
	case map[string]interface{}:
		if reference, ok := current["CNVBaselineFix.reference"].(string); ok && strings.HasSuffix(strings.ToLower(reference), ".cnn") {
			return reference
		}
		for childKey, child := range current {
			if childKey == "CNVBaselineFix.reference" {
				continue
			}
			if result := findBaselineOutputValue(child, childKey); result != "" {
				return result
			}
		}
	case []interface{}:
		for _, child := range current {
			if result := findBaselineOutputValue(child, key); result != "" {
				return result
			}
		}
	case string:
		if key == "CNVBaselineFix.reference" && strings.HasSuffix(strings.ToLower(current), ".cnn") {
			return current
		}
	}
	return ""
}

func findBaselineOutputFile(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	var result string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || result != "" {
			return nil
		}
		if entry.Type().IsRegular() && strings.HasSuffix(strings.ToLower(entry.Name()), ".cnvkit.ref.cnn") && !strings.Contains(strings.ToLower(entry.Name()), ".new_batch.") && strings.Contains(filepath.ToSlash(path), "call-UpdateReference/") {
			if info, statErr := os.Stat(path); statErr == nil && info.Size() > 0 {
				result = path
			}
		}
		return nil
	})
	return result
}

// Resolve only the final reference, from this tenant/task/attempt's archive.
func validatedCNVBaselineArchiveOutput(raw, prefix string, cfgHost string, objects []s3ObjectInfo) (string, error) {
	reference := baselineOutputFromJSON(raw)
	u, err := url.Parse(reference)
	if err != nil || u.Scheme != "https" || u.Host != cfgHost || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("CNV baseline archive has no valid final reference")
	}
	key := strings.TrimPrefix(u.Path, "/")
	if !strings.HasPrefix(key, prefix+"/") || strings.Contains(key, "/../") {
		return "", fmt.Errorf("CNV baseline reference does not belong to the current attempt")
	}
	for _, object := range objects {
		if object.Key == key && object.Size > 0 {
			return reference, nil
		}
	}
	return "", fmt.Errorf("CNV baseline final reference is missing or empty in object storage")
}

func (s *TaskService) registerArchivedCNVBaseline(task *model.Task, reference string) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		var current model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("uuid", "execution_attempt_id").Where("uuid = ? AND execution_attempt_id = ?", task.UUID, task.ExecutionAttemptID).
			First(&current).Error; err != nil {
			return fmt.Errorf("CNV baseline execution attempt is no longer current: %w", err)
		}
		result := tx.Model(&model.CNVBaseline{}).Where("task_uuid = ?", task.UUID).
			Updates(map[string]interface{}{"output_path": reference})
		if result.Error != nil {
			return fmt.Errorf("register archived CNV baseline: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("CNV baseline no longer exists")
		}
		return nil
	})
}
