package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/bioinfo/schema-platform/internal/config"
)

const (
	maxStatusUpdatesPerRequest = 1000
	maxStatusNameLength        = 64
	maxStatusTableNameLength   = 128
)

// StatusManager handles report/review status for parquet data
type StatusManager struct {
	cfg *config.Config
	mu  sync.RWMutex
}

// NewStatusManager creates a new status manager
func NewStatusManager(cfg *config.Config) *StatusManager {
	return &StatusManager{cfg: cfg}
}

// TableStatus represents status for rows in a table
type TableStatus struct {
	Rows []RowStatus `json:"rows"`
}

// RowStatus represents status for a single row
type RowStatus struct {
	RowIndex     int    `json:"row_index"`
	ReportStatus string `json:"report_status"`
	ReviewStatus string `json:"review_status"`
}

// StatusData represents the full status data structure
type StatusData struct {
	UUID   string                  `json:"uuid"`
	Tables map[string]*TableStatus `json:"tables"`
}

// StatusUpdate represents a status update request
type StatusUpdate struct {
	Table        string `json:"table"`
	RowIndex     int    `json:"row_index"`
	ReportStatus string `json:"report_status"`
	ReviewStatus string `json:"review_status"`
}

// GetStatusFilePath returns the path to status.json for a UUID
func (s *StatusManager) GetStatusFilePath(uuid string) string {
	archiveDir := filepath.Join(s.cfg.Task.ArchiveDir, uuid)
	return filepath.Join(archiveDir, "status.json")
}

// GetStatus retrieves status data for a UUID (public, acquires read lock)
func (s *StatusManager) GetStatus(uuid string) (*StatusData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.readStatus(uuid)
}

// readStatus reads status data without acquiring the lock (private, for use by locked methods)
func (s *StatusManager) readStatus(uuid string) (*StatusData, error) {
	statusPath := s.GetStatusFilePath(uuid)

	// If status file doesn't exist, return empty structure
	if _, err := os.Stat(statusPath); os.IsNotExist(err) {
		return &StatusData{
			UUID:   uuid,
			Tables: make(map[string]*TableStatus),
		}, nil
	}

	data, err := os.ReadFile(statusPath)
	if err != nil {
		return nil, err
	}

	var status StatusData
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}

	return &status, nil
}

// UpdateStatus updates status for specific rows
func (s *StatusManager) UpdateStatus(uuid string, updates []StatusUpdate) error {
	if len(updates) > maxStatusUpdatesPerRequest {
		return fmt.Errorf("too many status updates: maximum is %d", maxStatusUpdatesPerRequest)
	}
	for i, update := range updates {
		if err := validateStatusUpdate(update); err != nil {
			return fmt.Errorf("invalid status update at index %d: %w", i, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Load existing status or create new (use readStatus to avoid deadlock)
	status, err := s.readStatus(uuid)
	if err != nil {
		return err
	}

	// Apply updates
	for _, update := range updates {
		tableName := strings.TrimSpace(update.Table)
		if status.Tables[tableName] == nil {
			status.Tables[tableName] = &TableStatus{Rows: []RowStatus{}}
		}

		// Find or create row status
		table := status.Tables[tableName]
		found := false
		for i, row := range table.Rows {
			if row.RowIndex == update.RowIndex {
				table.Rows[i] = RowStatus{
					RowIndex:     update.RowIndex,
					ReportStatus: update.ReportStatus,
					ReviewStatus: update.ReviewStatus,
				}
				found = true
				break
			}
		}
		if !found {
			table.Rows = append(table.Rows, RowStatus{
				RowIndex:     update.RowIndex,
				ReportStatus: update.ReportStatus,
				ReviewStatus: update.ReviewStatus,
			})
		}
	}

	// Save to file
	return s.saveStatus(uuid, status)
}

// InitializeStatus creates initial status structure based on parquet tables
func (s *StatusManager) InitializeStatus(uuid string, tables map[string]int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := &StatusData{
		UUID:   uuid,
		Tables: make(map[string]*TableStatus),
	}

	for tableName, rowCount := range tables {
		if err := validateStatusTableName(tableName); err != nil {
			return fmt.Errorf("invalid status table %q: %w", tableName, err)
		}
		if rowCount < 0 {
			return fmt.Errorf("invalid negative row count for table %q", tableName)
		}
		rows := make([]RowStatus, rowCount)
		for i := 0; i < rowCount; i++ {
			rows[i] = RowStatus{
				RowIndex:     i,
				ReportStatus: "",
				ReviewStatus: "",
			}
		}
		status.Tables[tableName] = &TableStatus{Rows: rows}
	}

	return s.saveStatus(uuid, status)
}

func validateStatusUpdate(update StatusUpdate) error {
	if err := validateStatusTableName(update.Table); err != nil {
		return err
	}
	if update.RowIndex < 0 {
		return fmt.Errorf("row_index must be non-negative")
	}
	if !isSafeStatusValue(update.ReportStatus) {
		return fmt.Errorf("report_status is too long or contains control characters")
	}
	if !isSafeStatusValue(update.ReviewStatus) {
		return fmt.Errorf("review_status is too long or contains control characters")
	}
	return nil
}

func validateStatusTableName(table string) error {
	table = strings.TrimSpace(table)
	if table == "" {
		return fmt.Errorf("table is required")
	}
	if len(table) > maxStatusTableNameLength {
		return fmt.Errorf("table is too long")
	}
	if table == "." || table == ".." || strings.ContainsAny(table, `/\`) {
		return fmt.Errorf("table contains invalid path characters")
	}
	for _, r := range table {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') {
			return fmt.Errorf("table contains invalid characters")
		}
	}
	return nil
}

func isSafeStatusValue(value string) bool {
	if len(value) > maxStatusNameLength {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// saveStatus saves status data to file
func (s *StatusManager) saveStatus(uuid string, status *StatusData) error {
	statusPath := s.GetStatusFilePath(uuid)

	// Ensure directory exists
	archiveDir := filepath.Dir(statusPath)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(statusPath, data, 0644)
}

// GetParquetData returns parquet data as JSON for frontend consumption
func (s *StatusManager) GetParquetData(uuid string) (map[string]interface{}, error) {
	archiveDir := filepath.Join(s.cfg.Task.ArchiveDir, uuid)
	parquetPath := filepath.Join(archiveDir, "combined_tables.parquet")

	// Check if parquet exists
	if _, err := os.Stat(parquetPath); os.IsNotExist(err) {
		return nil, err
	}

	// Use parquet-go reader to extract data
	// For simplicity, we return the parquet file path
	// Frontend can use a parquet viewer library
	result := map[string]interface{}{
		"uuid":         uuid,
		"parquet_path": parquetPath,
		"exists":       true,
	}

	return result, nil
}

// GetCombinedData returns parquet data merged with status
func (s *StatusManager) GetCombinedData(uuid string, parquetGen *ParquetGenerator) (map[string]interface{}, error) {
	// Get parquet schema preview (contains table structure)
	schemaPreview, err := parquetGen.GetParquetSchemaPreview(uuid, filepath.Join(s.cfg.Task.ArchiveDir, uuid))
	if err != nil {
		return nil, err
	}

	// Get status data
	status, err := s.GetStatus(uuid)
	if err != nil {
		return nil, err
	}

	// Combine
	result := map[string]interface{}{
		"uuid":   uuid,
		"schema": schemaPreview,
		"status": status,
	}

	return result, nil
}
