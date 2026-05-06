package service

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
)

// Archiver handles archiving completed task results
type Archiver struct {
	cfg        *config.Config
	parquetGen *ParquetGenerator
}

// NewArchiver creates a new archiver
func NewArchiver(cfg *config.Config) *Archiver {
	return &Archiver{
		cfg:        cfg,
		parquetGen: NewParquetGenerator(cfg),
	}
}

// ResolvedOutputs represents the structure of outputs.resolved.json
// Top-level keys are "pipeline.prefix" (e.g. "SingleWES.summary"),
// values contain file URLs and inline qc_result
type ResolvedOutputs map[string]ResolvedSummary

// ResolvedSummary is the value under each pipeline key
type ResolvedSummary map[string]interface{}

// ArchiveResult represents the result of archiving operation
type ArchiveResult struct {
	UUID       string   `json:"uuid"`
	ArchiveDir string   `json:"archive_dir"`
	Files      []string `json:"files"`
	Deleted    bool     `json:"deleted"`
	OutputDir  string   `json:"output_dir"`
	Success    bool     `json:"success"`
	Error      string   `json:"error,omitempty"`
}

// ArchiveTask archives completed task results
func (a *Archiver) ArchiveTask(task *model.Task) (*ArchiveResult, error) {
	result := &ArchiveResult{
		UUID:    task.UUID,
		Success: false,
	}

	// Only archive completed tasks
	if task.Status != model.TaskStatusCompleted {
		return result, fmt.Errorf("task is not completed, status: %s", task.Status)
	}

	// Find the latest execution directory via _LAST symlink
	uuidDir := filepath.Join(a.cfg.Task.OutputDir, task.UUID)
	result.OutputDir = uuidDir

	lastLink := filepath.Join(uuidDir, "_LAST")

	execDir, err := os.Readlink(lastLink)
	if err != nil {
		result.Error = fmt.Sprintf("failed to read _LAST symlink: %v", err)
		return result, fmt.Errorf(result.Error)
	}

	// Full path to execution directory
	execPath := filepath.Join(uuidDir, execDir)

	// Read outputs.resolved.json
	resolvedPath := filepath.Join(execPath, "outputs.resolved.json")
	resolvedData, err := os.ReadFile(resolvedPath)
	if err != nil {
		// Fallback to outputs.json if resolved doesn't exist
		outputsPath := filepath.Join(execPath, "outputs.json")
		resolvedData, err = os.ReadFile(outputsPath)
		if err != nil {
			result.Error = fmt.Sprintf("failed to read outputs.resolved.json or outputs.json: %v", err)
			return result, fmt.Errorf(result.Error)
		}
	}

	// Create archive directory: ArchiveDir/UUID/
	archiveDir := filepath.Join(a.cfg.Task.ArchiveDir, task.UUID)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create archive directory: %v", err)
		return result, fmt.Errorf(result.Error)
	}
	result.ArchiveDir = archiveDir

	// Copy outputs.resolved.json to archive as outputs.json for backward compat
	archiveOutputsPath := filepath.Join(archiveDir, "outputs.resolved.json")
	if err := copyFile(resolvedPath, archiveOutputsPath); err != nil {
		result.Error = fmt.Sprintf("failed to copy outputs.resolved.json: %v", err)
		return result, fmt.Errorf(result.Error)
	}
	result.Files = append(result.Files, "outputs.resolved.json")

	// Parse resolved outputs to extract file paths
	var parsed map[string]interface{}
	if err := json.Unmarshal(resolvedData, &parsed); err != nil {
		result.Error = fmt.Sprintf("failed to parse outputs.resolved.json: %v", err)
		return result, fmt.Errorf(result.Error)
	}

	// Extract file URLs from all summaries
	filesToArchive := a.extractResolvedFileURLs(parsed)
	for _, srcPath := range filesToArchive {
		fileName := filepath.Base(srcPath)
		dstPath := filepath.Join(archiveDir, fileName)

		// Handle duplicate filenames
		if a.fileExists(dstPath) {
			dirName := filepath.Base(filepath.Dir(srcPath))
			fileName = dirName + "_" + fileName
			dstPath = filepath.Join(archiveDir, fileName)
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			fmt.Printf("warning: failed to archive %s: %v\n", srcPath, err)
			continue
		}

		result.Files = append(result.Files, fileName)
	}

	// Copy workflow.log to archive
	workflowLog := filepath.Join(execPath, "workflow.log")
	if _, err := os.Stat(workflowLog); err == nil {
		dstLog := filepath.Join(archiveDir, "workflow.log")
		if err := copyFile(workflowLog, dstLog); err == nil {
			result.Files = append(result.Files, "workflow.log")
		}
	}

	result.Success = true

	// Generate parquet files from text data
	if a.parquetGen != nil && result.Success {
		go a.parquetGen.GenerateOnArchive(task, archiveDir)
	}

	// Cleanup output directory if configured
	if a.cfg.Task.ArchiveCleanup && result.Success {
		if err := a.cleanupOutputDir(uuidDir); err != nil {
			fmt.Printf("warning: failed to cleanup output directory %s: %v\n", uuidDir, err)
		} else {
			result.Deleted = true
			fmt.Printf("cleaned up output directory: %s\n", uuidDir)
		}
	}

	return result, nil
}

// extractResolvedFileURLs extracts file paths from outputs.resolved.json
// Skips inline objects like qc_result
func (a *Archiver) extractResolvedFileURLs(parsed map[string]interface{}) []string {
	var files []string

	for _, value := range parsed {
		switch v := value.(type) {
		case map[string]interface{}:
			// This is a summary object (e.g. SingleWES.summary)
			for key, fieldValue := range v {
				switch fv := fieldValue.(type) {
				case string:
					if strings.Contains(fv, "/") || a.isFileExtension(fv) {
						files = append(files, fv)
					}
				case []interface{}:
					for _, item := range fv {
						if str, ok := item.(string); ok {
							if strings.Contains(str, "/") || a.isFileExtension(str) {
								files = append(files, str)
							}
						}
					}
				default:
					// Skip non-string values (qc_result, etc.)
					_ = key
				}
			}
		case string:
			if strings.Contains(v, "/") || a.isFileExtension(v) {
				files = append(files, v)
			}
		}
	}

	return files
}

// isFileExtension checks if a string looks like a file path
func (a *Archiver) isFileExtension(s string) bool {
	exts := []string{".gz", ".vcf", ".bam", ".bai", ".fastq", ".fq", ".bed", ".txt", ".json", ".csv", ".tsv", ".pdf", ".html", ".zip", ".cnr", ".cns"}
	for _, ext := range exts {
		if strings.HasSuffix(strings.ToLower(s), ext) {
			return true
		}
	}
	return false
}

// fileExists checks if a file exists
func (a *Archiver) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

// ArchiveOnCompletion archives task when it completes successfully
func (a *Archiver) ArchiveOnCompletion(task *model.Task) {
	time.Sleep(2 * time.Second)

	if task.Status != model.TaskStatusCompleted {
		return
	}

	result, err := a.ArchiveTask(task)
	if err != nil || !result.Success {
		fmt.Printf("archive failed for task %s: %v\n", task.ID, err)
		return
	}

	fmt.Printf("archived task %s to %s, files: %v\n", task.ID, result.ArchiveDir, result.Files)
}

// GetConfig returns the config
func (a *Archiver) GetConfig() *config.Config {
	return a.cfg
}

// GetArchiveDir returns the archive directory path for a UUID
func (a *Archiver) GetArchiveDir(uuid string) string {
	if a.cfg.Task.ArchiveDir == "" {
		return ""
	}
	return filepath.Join(a.cfg.Task.ArchiveDir, uuid)
}

// ListArchivedFiles lists files in the archive directory for a UUID
func (a *Archiver) ListArchivedFiles(uuid string) ([]string, error) {
	archiveDir := a.GetArchiveDir(uuid)
	if archiveDir == "" {
		return nil, fmt.Errorf("archive directory not configured")
	}

	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("archive not found for UUID: %s", uuid)
	}

	files := []string{}
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read archive directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}

	return files, nil
}

// OutputQueryResult represents the result of querying an output by key
type OutputQueryResult struct {
	UUID         string      `json:"uuid"`
	Key          string      `json:"key"`
	Value        interface{} `json:"value"`
	Path         string      `json:"path,omitempty"`
	Archived     bool        `json:"archived"`
	ArchivePath  string      `json:"archive_path,omitempty"`
	Exists       bool        `json:"exists"`
}

// QueryOutputByKey queries an output value by key from archived outputs.resolved.json
func (a *Archiver) QueryOutputByKey(uuid string, key string) (*OutputQueryResult, error) {
	result := &OutputQueryResult{
		UUID:     uuid,
		Key:      key,
		Archived: false,
		Exists:   false,
	}

	archiveDir := a.GetArchiveDir(uuid)
	if archiveDir == "" {
		return result, fmt.Errorf("archive directory not configured")
	}

	// Read archived outputs.resolved.json
	resolvedPath := filepath.Join(archiveDir, "outputs.resolved.json")
	resolvedData, err := os.ReadFile(resolvedPath)
	if err != nil {
		return result, fmt.Errorf("outputs.resolved.json not found in archive for UUID: %s", uuid)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(resolvedData, &parsed); err != nil {
		return result, fmt.Errorf("failed to parse outputs.resolved.json: %w", err)
	}

	// Navigate key path (e.g. "SingleWES.summary.snp_indel" or "snp_indel")
	keyParts := strings.Split(key, ".")

	var current interface{} = parsed
	for _, part := range keyParts {
		switch m := current.(type) {
		case map[string]interface{}:
			if val, ok := m[part]; ok {
				current = val
			} else {
				return result, fmt.Errorf("key '%s' not found", key)
			}
		default:
			return result, fmt.Errorf("invalid key path '%s'", key)
		}
	}

	result.Value = current
	result.Archived = true

	// If value is a file path, check if it's archived
	switch v := current.(type) {
	case string:
		if a.isFileExtension(v) || strings.Contains(v, "/") {
			result.Path = v
			fileName := filepath.Base(v)
			archiveFilePath := filepath.Join(archiveDir, fileName)
			if _, err := os.Stat(archiveFilePath); err == nil {
				result.ArchivePath = archiveFilePath
				result.Exists = true
			} else {
				dirName := filepath.Base(filepath.Dir(v))
				altFileName := dirName + "_" + fileName
				altArchivePath := filepath.Join(archiveDir, altFileName)
				if _, err := os.Stat(altArchivePath); err == nil {
					result.ArchivePath = altArchivePath
					result.Exists = true
				}
			}
		}
	}

	return result, nil
}

// ListOutputKeys lists all available output keys from archived outputs.resolved.json
func (a *Archiver) ListOutputKeys(uuid string) ([]string, error) {
	archiveDir := a.GetArchiveDir(uuid)
	if archiveDir == "" {
		return nil, fmt.Errorf("archive directory not configured")
	}

	resolvedPath := filepath.Join(archiveDir, "outputs.resolved.json")
	resolvedData, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("outputs.resolved.json not found in archive for UUID: %s", uuid)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(resolvedData, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse outputs.resolved.json: %w", err)
	}

	keys := []string{}
	a.collectResolvedKeys(parsed, "", &keys)
	return keys, nil
}

// collectResolvedKeys recursively collects all keys
func (a *Archiver) collectResolvedKeys(m map[string]interface{}, prefix string, keys *[]string) {
	for key, value := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			a.collectResolvedKeys(v, fullKey, keys)
		default:
			*keys = append(*keys, fullKey)
		}
	}
}

// cleanupOutputDir removes the output directory after successful archiving
func (a *Archiver) cleanupOutputDir(uuidDir string) error {
	archiveDir := filepath.Join(a.cfg.Task.ArchiveDir, filepath.Base(uuidDir))
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return fmt.Errorf("archive directory does not exist, refusing to delete output")
	}

	archiveOutputs := filepath.Join(archiveDir, "outputs.resolved.json")
	if _, err := os.Stat(archiveOutputs); os.IsNotExist(err) {
		// Also check for old format
		archiveOutputs = filepath.Join(archiveDir, "outputs.json")
		if _, err := os.Stat(archiveOutputs); os.IsNotExist(err) {
			return fmt.Errorf("no outputs file found in archive, refusing to delete output")
		}
	}

	return os.RemoveAll(uuidDir)
}
