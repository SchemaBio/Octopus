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
	cfg          *config.Config
	parquetGen   *ParquetGenerator
}

// NewArchiver creates a new archiver
func NewArchiver(cfg *config.Config) *Archiver {
	return &Archiver{
		cfg:        cfg,
		parquetGen: NewParquetGenerator(cfg),
	}
}

// OutputsJSON represents the structure of miniwdl outputs.json
type OutputsJSON struct {
	Outputs map[string]interface{} `json:"outputs"`
	Dir     string                 `json:"dir"`
}

// ArchiveResult represents the result of archiving operation
type ArchiveResult struct {
	UUID        string   `json:"uuid"`
	ArchiveDir  string   `json:"archive_dir"`
	Files       []string `json:"files"`
	Deleted     bool     `json:"deleted"`       // Whether output directory was deleted
	OutputDir   string   `json:"output_dir"`    // Original output directory (before deletion)
	Success     bool     `json:"success"`
	Error       string   `json:"error,omitempty"`
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

	// Read outputs.json
	outputsPath := filepath.Join(execPath, "outputs.json")
	outputsData, err := os.ReadFile(outputsPath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to read outputs.json: %v", err)
		return result, fmt.Errorf(result.Error)
	}

	var outputs OutputsJSON
	if err := json.Unmarshal(outputsData, &outputs); err != nil {
		result.Error = fmt.Sprintf("failed to parse outputs.json: %v", err)
		return result, fmt.Errorf(result.Error)
	}

	// Create archive directory: ArchiveDir/UUID/
	archiveDir := filepath.Join(a.cfg.Task.ArchiveDir, task.UUID)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create archive directory: %v", err)
		return result, fmt.Errorf(result.Error)
	}
	result.ArchiveDir = archiveDir

	// Copy outputs.json to archive
	archiveOutputsPath := filepath.Join(archiveDir, "outputs.json")
	if err := copyFile(outputsPath, archiveOutputsPath); err != nil {
		result.Error = fmt.Sprintf("failed to copy outputs.json: %v", err)
		return result, fmt.Errorf(result.Error)
	}
	result.Files = append(result.Files, "outputs.json")

	// Extract and copy output files from outputs.json
	filesToArchive := a.extractOutputFiles(outputs.Outputs)
	for _, srcPath := range filesToArchive {
		// Get relative path or filename
		fileName := filepath.Base(srcPath)

		// Destination path in archive
		dstPath := filepath.Join(archiveDir, fileName)

		// Handle duplicate filenames by using subdirectories based on output key
		if len(result.Files) > 1 && fileName == filepath.Base(filesToArchive[0]) {
			// Use a unique name for duplicate basenames
			fileName = filepath.Base(filepath.Dir(srcPath)) + "_" + fileName
			dstPath = filepath.Join(archiveDir, fileName)
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			// Log error but continue with other files
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

// extractOutputFiles extracts file paths from outputs.json values
func (a *Archiver) extractOutputFiles(outputs map[string]interface{}) []string {
	var files []string

	for key, value := range outputs {
		switch v := value.(type) {
		case string:
			// Check if it's a file path (contains / or .gz, .vcf, .bam, etc.)
			if strings.Contains(v, "/") || a.isFileExtension(v) {
				files = append(files, v)
			}
		case []interface{}:
			// Array of files
			for _, item := range v {
				if str, ok := item.(string); ok {
					if strings.Contains(str, "/") || a.isFileExtension(str) {
						files = append(files, str)
					}
				}
			}
		case map[string]interface{}:
			// Nested structure - extract recursively
			files = append(files, a.extractOutputFiles(v)...)
		}
	}

	return files
}

// isFileExtension checks if a string looks like a file path
func (a *Archiver) isFileExtension(s string) bool {
	exts := []string{".gz", ".vcf", ".bam", ".bai", ".fastq", ".fq", ".bed", ".txt", ".json", ".csv", ".tsv", ".pdf", ".html", ".zip"}
	for _, ext := range exts {
		if strings.HasSuffix(strings.ToLower(s), ext) {
			return true
		}
	}
	return false
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	// Check if source exists
	if _, err := os.Stat(src); err != nil {
		return err
	}

	// Open source
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Create destination
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy content
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Preserve file mode
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

// ArchiveOnCompletion archives task when it completes successfully
// This is called after task execution finishes
func (a *Archiver) ArchiveOnCompletion(task *model.Task) {
	// Wait a moment for outputs.json to be fully written
	time.Sleep(2 * time.Second)

	// Check if task completed successfully
	if task.Status != model.TaskStatusCompleted {
		return
	}

	// Perform archiving
	result, err := a.ArchiveTask(task)
	if err != nil || !result.Success {
		fmt.Printf("archive failed for task %s: %v\n", task.ID, err)
		return
	}

	fmt.Printf("archived task %s to %s, files: %v\n", task.ID, result.ArchiveDir, result.Files)
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

	// Check if directory exists
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("archive not found for UUID: %s", uuid)
	}

	// List files
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
	UUID      string      `json:"uuid"`
	Key       string      `json:"key"`
	Value     interface{} `json:"value"`
	Path      string      `json:"path,omitempty"`      // Original file path
	Archived  bool        `json:"archived"`
	ArchivePath string    `json:"archive_path,omitempty"` // Path in archive directory
	Exists    bool        `json:"exists"`              // Whether file exists in archive
}

// QueryOutputByKey queries an output value by key from archived outputs.json
// Key format: "output_name" or "outputs.output_name" (nested keys supported)
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

	// Read archived outputs.json
	outputsPath := filepath.Join(archiveDir, "outputs.json")
	outputsData, err := os.ReadFile(outputsPath)
	if err != nil {
		return result, fmt.Errorf("outputs.json not found in archive for UUID: %s", uuid)
	}

	var outputs OutputsJSON
	if err := json.Unmarshal(outputsData, &outputs); err != nil {
		return result, fmt.Errorf("failed to parse outputs.json: %w", err)
	}

	// Parse key (handle nested keys like "outputs.gvcf" or "result.vcf")
	keyParts := strings.Split(key, ".")

	// Navigate to the target key
	var current interface{} = outputs.Outputs
	for i, part := range keyParts {
		// Skip "outputs" prefix if present
		if i == 0 && part == "outputs" && len(keyParts) > 1 {
			continue
		}

		switch m := current.(type) {
		case map[string]interface{}:
			if val, ok := m[part]; ok {
				current = val
			} else {
				return result, fmt.Errorf("key '%s' not found in outputs", key)
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

			// Determine archived file path
			// The archived file uses basename from original path
			fileName := filepath.Base(v)
			archiveFilePath := filepath.Join(archiveDir, fileName)

			// Check if file exists in archive
			if _, err := os.Stat(archiveFilePath); err == nil {
				result.ArchivePath = archiveFilePath
				result.Exists = true
			} else {
				// Try alternative naming (dirname_filename for duplicates)
				dirName := filepath.Base(filepath.Dir(v))
				altFileName := dirName + "_" + fileName
				altArchivePath := filepath.Join(archiveDir, altFileName)
				if _, err := os.Stat(altArchivePath); err == nil {
					result.ArchivePath = altArchivePath
					result.Exists = true
				}
			}
		}
	case []interface{}:
		// Array of file paths
		var paths []string
		var archivePaths []string
		for _, item := range v {
			if str, ok := item.(string); ok {
				if a.isFileExtension(str) || strings.Contains(str, "/") {
					paths = append(paths, str)
					fileName := filepath.Base(str)
					archiveFilePath := filepath.Join(archiveDir, fileName)
					if _, err := os.Stat(archiveFilePath); err == nil {
						archivePaths = append(archivePaths, archiveFilePath)
					}
				}
			}
		}
		if len(paths) > 0 {
			result.Path = strings.Join(paths, ";")
			if len(archivePaths) > 0 {
				result.ArchivePath = strings.Join(archivePaths, ";")
				result.Exists = len(archivePaths) == len(paths)
			}
		}
	}

	return result, nil
}

// ListOutputKeys lists all available output keys from archived outputs.json
func (a *Archiver) ListOutputKeys(uuid string) ([]string, error) {
	archiveDir := a.GetArchiveDir(uuid)
	if archiveDir == "" {
		return nil, fmt.Errorf("archive directory not configured")
	}

	outputsPath := filepath.Join(archiveDir, "outputs.json")
	outputsData, err := os.ReadFile(outputsPath)
	if err != nil {
		return nil, fmt.Errorf("outputs.json not found in archive for UUID: %s", uuid)
	}

	var outputs OutputsJSON
	if err := json.Unmarshal(outputsData, &outputs); err != nil {
		return nil, fmt.Errorf("failed to parse outputs.json: %w", err)
	}

	keys := []string{}
	a.collectKeys(outputs.Outputs, "", &keys)
	return keys, nil
}

// collectKeys recursively collects all keys from a map
func (a *Archiver) collectKeys(m map[string]interface{}, prefix string, keys *[]string) {
	for key, value := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			a.collectKeys(v, fullKey, keys)
		default:
			*keys = append(*keys, fullKey)
		}
	}
}

// cleanupOutputDir removes the output directory after successful archiving
func (a *Archiver) cleanupOutputDir(uuidDir string) error {
	// Safety check: only delete if archive exists
	archiveDir := filepath.Join(a.cfg.Task.ArchiveDir, filepath.Base(uuidDir))
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return fmt.Errorf("archive directory does not exist, refusing to delete output")
	}

	// Check if outputs.json exists in archive (minimum requirement)
	archiveOutputs := filepath.Join(archiveDir, "outputs.json")
	if _, err := os.Stat(archiveOutputs); os.IsNotExist(err) {
		return fmt.Errorf("outputs.json not found in archive, refusing to delete output")
	}

	// Remove the entire UUID directory
	return os.RemoveAll(uuidDir)
}