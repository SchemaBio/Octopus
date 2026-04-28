package service

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/xitongsys/parquet-go/writer"
)

// ParquetGenerator handles converting text files to parquet format
type ParquetGenerator struct {
	cfg *config.Config
}

// NewParquetGenerator creates a new parquet generator
func NewParquetGenerator(cfg *config.Config) *ParquetGenerator {
	return &ParquetGenerator{cfg: cfg}
}

// ParquetResult represents the result of parquet generation
type ParquetResult struct {
	UUID        string                 `json:"uuid"`
	ParquetPath string                 `json:"parquet_path"`
	Files       []ParquetFileInfo      `json:"files"`
	Schema      map[string]interface{} `json:"schema"` // Generated schema preview
	Success     bool                   `json:"success"`
	Error       string                 `json:"error,omitempty"`
}

// ParquetFileInfo represents info about a converted file
type ParquetFileInfo struct {
	SourceFile string   `json:"source_file"` // Original file name
	Field      string   `json:"field"`       // Field name in parquet (normalized filename)
	Rows       int      `json:"rows"`        // Number of rows
	Columns    int      `json:"columns"`     // Number of columns
	Headers    []string `json:"headers"`     // Column headers
	HasHeader  bool     `json:"has_header"`  // Whether source has header
}

// CombinedRecord represents a combined record for parquet
// Structure: uuid + each file as nested array (status stored separately in status.json)
type CombinedRecord struct {
	Uuid   string                `json:"uuid"`
	Tables map[string][]TableRow `json:"tables"` // Each file as a nested table
}

// TableRow represents a single row from a text file
type TableRow struct {
	RowData map[string]string `json:"row_data"`
}

// GenerateParquet generates a single parquet file from multiple archived text files
func (p *ParquetGenerator) GenerateParquet(uuid string, archiveDir string) (*ParquetResult, error) {
	result := &ParquetResult{
		UUID:    uuid,
		Success: false,
	}

	if !p.cfg.Parquet.Enabled {
		return result, fmt.Errorf("parquet generation is disabled")
	}

	// Find all text files matching patterns
	files := p.findTextFiles(archiveDir)

	if len(files) == 0 {
		result.Error = "no text files found to convert"
		return result, fmt.Errorf(result.Error)
	}

	// Parse all files and collect data
	tables := make(map[string][]TableRow)
	schemaPreview := make(map[string]interface{})

	for _, file := range files {
		info, rows, err := p.parseFile(file)
		if err != nil {
			fmt.Printf("warning: failed to parse %s: %v\n", file, err)
			continue
		}

		// Normalize field name from filename
		fieldName := p.normalizeFieldName(info.SourceFile)
		tables[fieldName] = rows
		result.Files = append(result.Files, info)

		// Add to schema preview
		schemaPreview[fieldName] = map[string]interface{}{
			"rows":    len(rows),
			"columns": info.Headers,
		}
	}
	result.Schema = schemaPreview

	if len(tables) == 0 {
		result.Error = "no files successfully parsed"
		return result, fmt.Errorf(result.Error)
	}

	// Generate parquet file path (single file per UUID)
	parquetPath := filepath.Join(archiveDir, "combined_tables.parquet")
	result.ParquetPath = parquetPath

	// Write combined data to parquet
	if err := p.writeCombinedParquet(parquetPath, uuid, tables); err != nil {
		result.Error = fmt.Sprintf("failed to write parquet: %v", err)
		return result, fmt.Errorf(result.Error)
	}

	result.Success = true
	return result, nil
}

// findTextFiles finds all text files matching configured patterns
func (p *ParquetGenerator) findTextFiles(dir string) []string {
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip outputs.json, workflow.log, status.json, and parquet files
		if name == "outputs.json" || name == "workflow.log" || name == "status.json" || strings.HasSuffix(name, ".parquet") {
			continue
		}

		// Check if file matches any pattern
		for _, pattern := range p.cfg.Parquet.FilePatterns {
			if matched, _ := filepath.Match(pattern, name); matched {
				files = append(files, filepath.Join(dir, name))
				break
			}
			// Also check by extension
			ext := filepath.Ext(name)
			if pattern == "*"+ext {
				files = append(files, filepath.Join(dir, name))
				break
			}
		}
	}

	return files
}

// normalizeFieldName converts filename to a valid field name
func (p *ParquetGenerator) normalizeFieldName(filename string) string {
	// Remove extension
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	// Replace spaces and special chars with underscores
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")

	// Convert to lowercase
	name = strings.ToLower(name)

	// Remove non-alphanumeric chars except underscore
	result := ""
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			result += string(c)
		}
	}

	return result
}

// parseFile parses a text file and returns rows
func (p *ParquetGenerator) parseFile(path string) (ParquetFileInfo, []TableRow, error) {
	info := ParquetFileInfo{
		SourceFile: filepath.Base(path),
	}

	file, err := os.Open(path)
	if err != nil {
		return info, nil, err
	}
	defer file.Close()

	// Detect delimiter
	delimiter := p.detectDelimiter(file)
	file.Seek(0, 0)

	reader := csv.NewReader(file)
	reader.Comma = delimiter
	reader.LazyQuotes = true

	var headers []string
	var rows []TableRow

	// Read first line as potential header
	firstRow, err := reader.Read()
	if err != nil {
		return info, nil, err
	}

	// Check if first row looks like a header
	if p.looksLikeHeader(firstRow) {
		headers = firstRow
		info.HasHeader = true
	} else {
		headers = p.generateHeaders(len(firstRow))
		info.HasHeader = false
		// First row is data
		rows = append(rows, TableRow{RowData: p.rowToMap(headers, firstRow)})
	}

	info.Headers = headers
	info.Columns = len(headers)

	// Read remaining rows
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		rows = append(rows, TableRow{RowData: p.rowToMap(headers, row)})
	}

	info.Rows = len(rows)
	return info, rows, nil
}

// detectDelimiter detects the delimiter used in the file
func (p *ParquetGenerator) detectDelimiter(file *os.File) rune {
	scanner := bufio.NewScanner(file)
	scanner.Scan()
	line := scanner.Text()

	delimiters := map[rune]int{
		',':  strings.Count(line, ","),
		'\t': strings.Count(line, "\t"),
		'|':  strings.Count(line, "|"),
	}

	maxCount := 0
	detected := ','

	for delim, count := range delimiters {
		if count > maxCount {
			maxCount = count
			detected = delim
		}
	}

	return detected
}

// looksLikeHeader checks if a row looks like a header row
func (p *ParquetGenerator) looksLikeHeader(row []string) bool {
	nonNumeric := 0
	for _, val := range row {
		if !p.isNumeric(strings.TrimSpace(val)) {
			nonNumeric++
		}
	}
	return nonNumeric > len(row)/2
}

// isNumeric checks if a string is purely numeric
func (p *ParquetGenerator) isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != '-' && c != '+' {
			return false
		}
	}
	return true
}

// generateHeaders generates generic column headers
func (p *ParquetGenerator) generateHeaders(count int) []string {
	headers := make([]string, count)
	for i := 0; i < count; i++ {
		headers[i] = fmt.Sprintf("col_%d", i+1)
	}
	return headers
}

// rowToMap converts a row to a map with headers as keys
func (p *ParquetGenerator) rowToMap(headers []string, row []string) map[string]string {
	rowMap := make(map[string]string)
	for i, header := range headers {
		if i < len(row) {
			rowMap[header] = row[i]
		} else {
			rowMap[header] = ""
		}
	}
	return rowMap
}

// writeCombinedParquet writes combined data to a single parquet file
func (p *ParquetGenerator) writeCombinedParquet(path string, uuid string, tables map[string][]TableRow) error {
	// Create the combined record (status stored separately in status.json)
	record := CombinedRecord{
		Uuid:   uuid,
		Tables: tables,
	}

	// Convert to JSON for parquet-go JSON writer
	jsonData, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// Write using parquet-go
	fw, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fw.Close()

	// Create schema dynamically based on the tables
	schema := p.createCombinedSchema(tables)

	pw, err := writer.NewJSONWriter(schema, fw, 4)
	if err != nil {
		return fmt.Errorf("failed to create parquet writer: %w", err)
	}

	// Write the JSON data (JSONWriter expects JSON bytes, not struct)
	if err := pw.Write(string(jsonData)); err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}

	if err := pw.WriteStop(); err != nil {
		return fmt.Errorf("failed to finalize parquet: %w", err)
	}

	return nil
}

// createCombinedSchema creates a parquet schema for combined data
func (p *ParquetGenerator) createCombinedSchema(tables map[string][]TableRow) string {
	// Build schema with uuid and each table as a nested list
	schema := `message CombinedRecord {
		required binary uuid (UTF8);
`

	// Add each table as a nested group (list of rows)
	for fieldName, tableRows := range tables {
		if len(tableRows) == 0 {
			continue
		}

		// Get headers from first row
		headers := make([]string, 0)
		for key := range tableRows[0].RowData {
			headers = append(headers, key)
		}

		// Create nested structure for this table
		schema += fmt.Sprintf(`
	group %s (LIST) {
		repeated group list {
			group element {
`, fieldName)

		// Add each column as a field
		for _, header := range headers {
			normalizedHeader := p.normalizeFieldName(header)
			schema += fmt.Sprintf("			optional binary %s (UTF8);\n", normalizedHeader)
		}

		schema += `		}
		}
	}
`
	}

	schema += "}"
	return schema
}

// GenerateOnArchive generates parquet when archiving completes
func (p *ParquetGenerator) GenerateOnArchive(task *model.Task, archiveDir string) {
	if !p.cfg.Parquet.Enabled {
		return
	}

	result, err := p.GenerateParquet(task.UUID, archiveDir)
	if err != nil {
		fmt.Printf("parquet generation failed for task %s: %v\n", task.ID, err)
		return
	}

	fmt.Printf("generated combined parquet for task %s: %d tables from %d files\n",
		task.ID, len(result.Files), len(result.Files))
}

// GetParquetSchemaPreview returns a preview of the expected parquet structure
func (p *ParquetGenerator) GetParquetSchemaPreview(uuid string, archiveDir string) (map[string]interface{}, error) {
	files := p.findTextFiles(archiveDir)

	schema := map[string]interface{}{
		"uuid":   uuid,
		"tables": make(map[string]interface{}),
	}

	for _, file := range files {
		info, _, err := p.parseFile(file)
		if err != nil {
			continue
		}

		fieldName := p.normalizeFieldName(info.SourceFile)
		schema["tables"].(map[string]interface{})[fieldName] = map[string]interface{}{
			"type":            "array",
			"columns":         info.Headers,
			"sample_row_count": info.Rows,
		}
	}

	return schema, nil
}