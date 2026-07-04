package handler

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

// ParquetHandler handles parquet data API requests
type ParquetHandler struct {
	cfg      *config.Config
	taskRepo *repository.TaskRepository
	reader   *service.ParquetReader
}

// NewParquetHandler creates a new parquet handler
func NewParquetHandler(cfg *config.Config) *ParquetHandler {
	return &ParquetHandler{
		cfg:      cfg,
		taskRepo: repository.NewTaskRepository(),
		reader:   service.NewParquetReader(),
	}
}

// ListTables returns available parquet tables for a task
// GET /api/v1/tasks/:id/parquet
func (h *ParquetHandler) ListTables(c *gin.Context) {
	taskID := c.Param("id")

	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}

	if task.OutputDir == "" {
		ErrorBadRequest(c, "Task has no output directory")
		return
	}

	tables, err := h.reader.ListParquetTables(task.OutputDir)
	if err != nil {
		ErrorInternal(c, "Failed to list parquet tables: "+err.Error())
		return
	}

	Success(c, gin.H{
		"task_id": taskID,
		"tables":  tables,
	})
}

// GetTableRows returns a page of rows from a specific parquet table
// GET /api/v1/tasks/:id/parquet/:table/rows?offset=0&limit=100
func (h *ParquetHandler) GetTableRows(c *gin.Context) {
	taskID := c.Param("id")
	tableName := c.Param("table")
	if err := validateParquetTableName(tableName); err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}

	offsetStr := c.DefaultQuery("offset", "0")
	limitStr := c.DefaultQuery("limit", "100")

	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil || offset < 0 {
		ErrorBadRequest(c, "Invalid offset")
		return
	}

	limit, err := strconv.ParseInt(limitStr, 10, 64)
	if err != nil || limit <= 0 {
		ErrorBadRequest(c, "Invalid limit")
		return
	}

	task, ok := requireTaskAccess(c, h.taskRepo, taskID)
	if !ok {
		return
	}

	if task.OutputDir == "" {
		ErrorBadRequest(c, "Task has no output directory")
		return
	}

	result, err := h.reader.ReadPage(filepath.Join(task.OutputDir, tableName+".parquet"), offset, limit)
	if err != nil {
		ErrorNotFound(c, "Parquet table not found: "+tableName)
		return
	}

	result.Table = tableName
	Success(c, result)
}

func validateParquetTableName(tableName string) error {
	if tableName == "" || tableName == "." || tableName == ".." {
		return fmt.Errorf("invalid parquet table name")
	}
	if strings.ContainsAny(tableName, `/\`) || filepath.IsAbs(tableName) {
		return fmt.Errorf("invalid parquet table name")
	}
	return nil
}
