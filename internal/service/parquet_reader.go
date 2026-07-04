package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/reader"
)

const MaxParquetPageLimit = 1000

// ParquetPageResult holds a page of rows from a parquet file
type ParquetPageResult struct {
	Table     string                   `json:"table"`
	Columns   []ParquetColumn          `json:"columns"`
	Rows      []map[string]interface{} `json:"rows"`
	TotalRows int64                    `json:"total_rows"`
	Offset    int64                    `json:"offset"`
	Limit     int64                    `json:"limit"`
}

// ParquetColumn describes a column in the result
type ParquetColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ParquetReader provides paged reading of flat-row parquet files
type ParquetReader struct{}

// NewParquetReader creates a new parquet reader
func NewParquetReader() *ParquetReader {
	return &ParquetReader{}
}

// ReadPage reads a page of rows from a parquet file.
// filePath: full path to the .parquet file
// offset: start row (0-indexed)
// limit: max rows to return
func (pr *ParquetReader) ReadPage(filePath string, offset, limit int64) (*ParquetPageResult, error) {
	safePath, err := resolveParquetRegularFile(filepath.Dir(filePath), filePath)
	if err != nil {
		return nil, fmt.Errorf("parquet file not found: %s", filePath)
	}

	fr, err := local.NewLocalFileReader(safePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file: %w", err)
	}
	defer fr.Close()

	// Read into generic []map
	prReader, err := reader.NewParquetReader(fr, nil, 4)
	if err != nil {
		return nil, fmt.Errorf("failed to create parquet reader: %w", err)
	}
	defer prReader.ReadStop()

	totalRows := int64(prReader.GetNumRows())

	offset, limit = normalizeParquetPage(totalRows, offset, limit)
	var pageRows []map[string]interface{}
	if limit > 0 {
		if offset > 0 {
			if err := prReader.SkipRows(offset); err != nil {
				return nil, fmt.Errorf("failed to seek parquet data: %w", err)
			}
		}
		rawRows, err := prReader.ReadByNumber(int(limit))
		if err != nil {
			return nil, fmt.Errorf("failed to read parquet data: %w", err)
		}
		pageRows, err = parquetRowsToMaps(rawRows)
		if err != nil {
			return nil, fmt.Errorf("failed to convert parquet data: %w", err)
		}
	}

	if pageRows == nil {
		pageRows = []map[string]interface{}{}
	}

	// Clean up values: convert byte arrays to strings (common in parquet-go)
	cleanedRows := make([]map[string]interface{}, len(pageRows))
	for i, row := range pageRows {
		cleanedRows[i] = cleanRowValues(row)
	}

	// Extract columns from first row
	columns := pr.extractColumns(pageRows)

	table := strings.TrimSuffix(filepath.Base(safePath), ".parquet")

	result := &ParquetPageResult{
		Table:     table,
		Columns:   columns,
		Rows:      cleanedRows,
		TotalRows: totalRows,
		Offset:    offset,
		Limit:     limit,
	}

	return result, nil
}

func normalizeParquetPage(totalRows, offset, limit int64) (int64, int64) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > MaxParquetPageLimit {
		limit = 100
	}
	if totalRows <= 0 || offset >= totalRows {
		return offset, 0
	}
	if offset+limit > totalRows {
		limit = totalRows - offset
	}
	return offset, limit
}

func parquetRowsToMaps(rawRows []interface{}) ([]map[string]interface{}, error) {
	rows := make([]map[string]interface{}, 0, len(rawRows))
	for _, raw := range rawRows {
		if raw == nil {
			continue
		}
		if row, ok := raw.(map[string]interface{}); ok {
			rows = append(rows, row)
			continue
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		var row map[string]interface{}
		if err := json.Unmarshal(b, &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func resolveParquetRegularFile(baseDir, filePath string) (string, error) {
	baseEval, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", err
	}
	fileEval, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return "", err
	}
	if !parquetPathInsideBase(baseEval, fileEval) {
		return "", fmt.Errorf("parquet file escapes output directory")
	}
	info, err := os.Stat(fileEval)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("parquet file is not regular")
	}
	return fileEval, nil
}

func parquetPathInsideBase(base, path string) bool {
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

// extractColumns extracts column metadata from the first row
func (pr *ParquetReader) extractColumns(rows []map[string]interface{}) []ParquetColumn {
	if len(rows) == 0 {
		return nil
	}

	firstRow := rows[0]
	columns := make([]ParquetColumn, 0, len(firstRow))
	for name, val := range firstRow {
		colType := inferType(val)
		columns = append(columns, ParquetColumn{Name: name, Type: colType})
	}
	return columns
}

// cleanRowValues cleans up values from parquet reading (converts []byte to string, etc.)
func cleanRowValues(row map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{}, len(row))
	for key, val := range row {
		cleaned[key] = normalizeValue(val)
	}
	return cleaned
}

// normalizeValue converts parquet-specific types to JSON-compatible types
func normalizeValue(val interface{}) interface{} {
	switch v := val.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case float64:
		return v
	case int64:
		return v
	case int32:
		return v
	case bool:
		return v
	case nil:
		return nil
	default:
		// Try JSON round-trip for complex types
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		var result interface{}
		json.Unmarshal(b, &result)
		return result
	}
}

// inferType returns a string representation of the value's type
func inferType(val interface{}) string {
	switch val.(type) {
	case []byte:
		return "string"
	case string:
		return "string"
	case float64:
		return "float64"
	case int64:
		return "int64"
	case int32:
		return "int32"
	case bool:
		return "bool"
	default:
		return "string"
	}
}

// FindParquetFiles finds all .parquet files in a directory
func (pr *ParquetReader) FindParquetFiles(dir string) ([]string, error) {
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".parquet") {
			path := filepath.Join(dir, entry.Name())
			safePath, err := resolveParquetRegularFile(dir, path)
			if err != nil {
				continue
			}
			files = append(files, safePath)
		}
	}

	return files, nil
}

// ListParquetTables lists available parquet tables in a task's output directory
func (pr *ParquetReader) ListParquetTables(outputDir string) ([]ParquetTableInfo, error) {
	files, err := pr.FindParquetFiles(outputDir)
	if err != nil {
		return nil, err
	}

	tables := make([]ParquetTableInfo, 0, len(files))
	for _, f := range files {
		tableName := strings.TrimSuffix(filepath.Base(f), ".parquet")
		if !IsSafeParquetTableName(tableName) {
			continue
		}
		totalRows, err := pr.getRowCount(f)
		if err != nil {
			totalRows = -1
		}
		tables = append(tables, ParquetTableInfo{
			Name:      tableName,
			TotalRows: totalRows,
		})
	}

	return tables, nil
}

// getRowCount quickly gets the row count of a parquet file
func (pr *ParquetReader) getRowCount(filePath string) (int64, error) {
	fr, err := local.NewLocalFileReader(filePath)
	if err != nil {
		return 0, err
	}
	defer fr.Close()

	prReader, err := reader.NewParquetReader(fr, nil, 1)
	if err != nil {
		return 0, err
	}
	defer prReader.ReadStop()

	return int64(prReader.GetNumRows()), nil
}

// ParquetTableInfo describes a parquet table available in a task directory
type ParquetTableInfo struct {
	Name      string `json:"name"`
	TotalRows int64  `json:"total_rows"`
}

// safeTableNameRe matches valid table names (alphanumeric, underscore, hyphen, dot).
var safeTableNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// IsSafeParquetTableName validates that a table name is safe for use in queries.
// Rejects empty names, path traversal attempts, and names with special characters.
func IsSafeParquetTableName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	return safeTableNameRe.MatchString(name)
}
