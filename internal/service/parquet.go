package service

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
)

// ParquetGenerator provides read-only schema preview for parquet data.
// Actual parquet generation during archive is handled by Sepiida Agent.
type ParquetGenerator struct {
	cfg *config.Config
}

func NewParquetGenerator(cfg *config.Config) *ParquetGenerator {
	return &ParquetGenerator{cfg: cfg}
}

// ParquetResult represents the result of parquet schema inspection
type ParquetResult struct {
	UUID        string                 `json:"uuid"`
	ParquetPath string                 `json:"parquet_path"`
	Files       []ParquetFileInfo      `json:"files"`
	Schema      map[string]interface{} `json:"schema"`
	Success     bool                   `json:"success"`
	Error       string                 `json:"error,omitempty"`
}

// ParquetFileInfo represents info about a file
type ParquetFileInfo struct {
	SourceFile string   `json:"source_file"`
	Field      string   `json:"field"`
	Rows       int      `json:"rows"`
	Columns    int      `json:"columns"`
	Headers    []string `json:"headers"`
	HasHeader  bool     `json:"has_header"`
}

// CombinedRecord represents a combined record for parquet (used for schema preview)
type CombinedRecord struct {
	Uuid   string                `json:"uuid"`
	Tables map[string][]TableRow `json:"tables"`
}

// TableRow represents a single row from a text file
type TableRow struct {
	RowData map[string]string `json:"row_data"`
}

// GetParquetSchemaPreview returns a preview of the parquet structure from text files
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
			"type":             "array",
			"columns":          info.Headers,
			"sample_row_count": info.Rows,
		}
	}

	return schema, nil
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
		// Skip non-data files
		if name == "outputs.json" || name == "outputs.resolved.json" ||
			name == "workflow.log" || name == "status.json" ||
			strings.HasSuffix(name, ".parquet") {
			continue
		}

		for _, pattern := range p.cfg.Parquet.FilePatterns {
			if matched, _ := filepath.Match(pattern, name); matched {
				files = append(files, filepath.Join(dir, name))
				break
			}
		}
	}

	return files
}

// parseFile parses a text file and returns info and rows
func (p *ParquetGenerator) parseFile(filePath string) (ParquetFileInfo, []TableRow, error) {
	info := ParquetFileInfo{
		SourceFile: filepath.Base(filePath),
	}

	f, err := os.Open(filePath)
	if err != nil {
		return info, nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	var headers []string
	var rows []TableRow
	firstLine := true

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Auto-detect delimiter
		delimiter := detectDelimiter(line)
		fields := splitLine(line, delimiter)

		if firstLine {
			if isHeader(fields) {
				headers = p.cleanHeaders(fields)
				info.HasHeader = true
			} else {
				headers = p.generateHeaders(len(fields))
				rowData := p.rowToMap(headers, fields)
				rows = append(rows, TableRow{RowData: rowData})
				info.HasHeader = false
			}
			firstLine = false
		} else {
			rowData := p.rowToMap(headers, fields)
			rows = append(rows, TableRow{RowData: rowData})
		}
	}

	if err := scanner.Err(); err != nil {
		return info, rows, err
	}

	info.Rows = len(rows)
	if len(headers) > 0 {
		info.Columns = len(headers)
	}
	info.Headers = headers

	return info, rows, nil
}

// detectDelimiter detects the most likely delimiter in a line
func detectDelimiter(line string) rune {
	delimiters := []rune{'\t', ',', '|'}
	maxCount := 0
	bestDelim := '\t'

	for _, d := range delimiters {
		count := 0
		for _, ch := range line {
			if ch == d {
				count++
			}
		}
		if count > maxCount {
			maxCount = count
			bestDelim = d
		}
	}

	return bestDelim
}

// splitLine splits a line by delimiter
func splitLine(line string, delimiter rune) []string {
	var fields []string
	current := ""
	inQuotes := false

	for _, ch := range line {
		if ch == '"' {
			inQuotes = !inQuotes
		} else if ch == delimiter && !inQuotes {
			fields = append(fields, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	fields = append(fields, current)
	return fields
}

// isHeader checks if a row looks like a header (contains common patterns)
func isHeader(fields []string) bool {
	for _, f := range fields {
		lower := strings.ToLower(f)
		if lower == "chr" || lower == "chromosome" || lower == "chrom" ||
			lower == "pos" || lower == "position" || lower == "start" ||
			lower == "ref" || lower == "alt" || lower == "gene" ||
			lower == "id" || lower == "name" || lower == "sample" {
			return true
		}
	}
	return false
}

func (p *ParquetGenerator) cleanHeaders(headers []string) []string {
	cleaned := make([]string, len(headers))
	for i, h := range headers {
		cleaned[i] = strings.Trim(strings.TrimSpace(h), "\"'")
	}
	return cleaned
}

func (p *ParquetGenerator) generateHeaders(count int) []string {
	headers := make([]string, count)
	for i := range headers {
		headers[i] = fmt.Sprintf("column_%d", i+1)
	}
	return headers
}

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

// normalizeFieldName converts a filename to a valid parquet field name
func (p *ParquetGenerator) normalizeFieldName(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return strings.ToLower(name)
}
