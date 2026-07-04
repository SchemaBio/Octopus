package service

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
)

// Archiver provides read-only access to archived task results.
// Sepiida Agent handles the actual archiving — Octopus only reads.
type Archiver struct {
	cfg *config.Config
}

const maxArchiveOutputsJSONBytes = 10 << 20

func NewArchiver(cfg *config.Config) *Archiver {
	return &Archiver{cfg: cfg}
}

// OutputQueryResult represents the result of querying an output by key
type OutputQueryResult struct {
	UUID        string      `json:"uuid"`
	Key         string      `json:"key"`
	Value       interface{} `json:"value"`
	Path        string      `json:"path,omitempty"`
	Archived    bool        `json:"archived"`
	ArchivePath string      `json:"archive_path,omitempty"`
	Exists      bool        `json:"exists"`
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
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			files = append(files, entry.Name())
		}
	}

	return files, nil
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

	resolvedData, err := a.readArchiveJSONFile(archiveDir, "outputs.resolved.json")
	if err != nil {
		return result, fmt.Errorf("outputs.resolved.json not found in archive for UUID: %s", uuid)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(resolvedData, &parsed); err != nil {
		return result, fmt.Errorf("failed to parse outputs.resolved.json: %w", err)
	}

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

	switch v := current.(type) {
	case string:
		if isFileExtension(v) || strings.Contains(v, "/") {
			result.Path = v
			fileName := filepath.Base(v)
			archiveFilePath := filepath.Join(archiveDir, fileName)
			if safePath, err := resolveArchiveRegularFile(archiveDir, archiveFilePath); err == nil {
				result.ArchivePath = safePath
				result.Exists = true
			} else {
				dirName := filepath.Base(filepath.Dir(v))
				altFileName := dirName + "_" + fileName
				altArchivePath := filepath.Join(archiveDir, altFileName)
				if safePath, err := resolveArchiveRegularFile(archiveDir, altArchivePath); err == nil {
					result.ArchivePath = safePath
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

	resolvedData, err := a.readArchiveJSONFile(archiveDir, "outputs.resolved.json")
	if err != nil {
		return nil, fmt.Errorf("outputs.resolved.json not found in archive for UUID: %s", uuid)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(resolvedData, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse outputs.resolved.json: %w", err)
	}

	keys := []string{}
	collectResolvedKeys(parsed, "", &keys)
	return keys, nil
}

// ReadOutputs reads the full archived outputs JSON for display purposes.
// It prefers outputs.resolved.json and falls back to outputs.json, while keeping
// the read bounded to the task archive directory and a fixed maximum size.
func (a *Archiver) ReadOutputs(uuid string) (map[string]interface{}, error) {
	archiveDir := a.GetArchiveDir(uuid)
	if archiveDir == "" {
		return nil, fmt.Errorf("archive directory not configured")
	}

	data, err := a.readArchiveJSONFile(archiveDir, "outputs.resolved.json")
	if err != nil {
		data, err = a.readArchiveJSONFile(archiveDir, "outputs.json")
	}
	if err != nil {
		return nil, err
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse outputs JSON: %w", err)
	}
	return parsed, nil
}

func collectResolvedKeys(m map[string]interface{}, prefix string, keys *[]string) {
	for key, value := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			collectResolvedKeys(v, fullKey, keys)
		default:
			*keys = append(*keys, fullKey)
		}
	}
}

func isFileExtension(s string) bool {
	exts := []string{".gz", ".vcf", ".bam", ".bai", ".fastq", ".fq", ".bed", ".txt", ".json", ".csv", ".tsv", ".pdf", ".html", ".zip", ".cnr", ".cns", ".parquet"}
	for _, ext := range exts {
		if strings.HasSuffix(strings.ToLower(s), ext) {
			return true
		}
	}
	return false
}

func (a *Archiver) readArchiveJSONFile(archiveDir, name string) ([]byte, error) {
	if name != filepath.Base(name) || name == "." || name == ".." {
		return nil, fmt.Errorf("invalid archive file name")
	}
	filePath, err := resolveArchiveRegularFile(archiveDir, filepath.Join(archiveDir, name))
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxArchiveOutputsJSONBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxArchiveOutputsJSONBytes {
		return nil, fmt.Errorf("archive JSON exceeds maximum size of %d MB", maxArchiveOutputsJSONBytes>>20)
	}
	return data, nil
}

func resolveArchiveRegularFile(archiveDir, filePath string) (string, error) {
	baseEval, err := filepath.EvalSymlinks(archiveDir)
	if err != nil {
		return "", err
	}
	fileEval, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return "", err
	}
	if !archivePathInsideBase(baseEval, fileEval) {
		return "", fmt.Errorf("archive file escapes archive directory")
	}
	info, err := os.Stat(fileEval)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("archive file is not regular")
	}
	return fileEval, nil
}

func archivePathInsideBase(base, path string) bool {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
