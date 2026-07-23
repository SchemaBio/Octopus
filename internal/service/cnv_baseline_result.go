package service

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
)

func (s *TaskService) syncCNVBaselineOutput(task *model.Task, outputsJSON string) {
	if task == nil || task.Template != "baseline" || task.Status != model.TaskStatusCompleted {
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
		for childKey, child := range current {
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
		lowerKey, lowerValue := strings.ToLower(key), strings.ToLower(current)
		if strings.Contains(lowerKey, "baseline") || strings.HasSuffix(lowerValue, ".cnn") {
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
		if entry.Type().IsRegular() && strings.HasSuffix(strings.ToLower(entry.Name()), ".cnn") {
			if info, statErr := os.Stat(path); statErr == nil && info.Size() > 0 {
				result = path
			}
		}
		return nil
	})
	return result
}
