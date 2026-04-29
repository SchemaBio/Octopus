package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/gin-gonic/gin"
)

type ExportHandler struct {
	cfg      *config.Config
	taskRepo *repository.TaskRepository
}

func NewExportHandler(cfg *config.Config) *ExportHandler {
	return &ExportHandler{
		cfg:      cfg,
		taskRepo: repository.NewTaskRepository(),
	}
}

// ExportExcel serves the Excel export file
func (h *ExportHandler) ExportExcel(c *gin.Context) {
	h.serveFile(c, "excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
}

// ExportParquet serves the Parquet export file
func (h *ExportHandler) ExportParquet(c *gin.Context) {
	h.serveFile(c, "parquet", "application/octet-stream")
}

// ExportVCF serves the SNP/InDel VCF file
func (h *ExportHandler) ExportVCF(c *gin.Context) {
	h.serveFile(c, "vcf", "text/x-vcard")
}

// ExportMTVCF serves the mitochondrial VCF file
func (h *ExportHandler) ExportMTVCF(c *gin.Context) {
	h.serveFile(c, "mt-vcf", "text/x-vcard")
}

// serveFile is a generic file serving helper
func (h *ExportHandler) serveFile(c *gin.Context, fileType, contentType string) {
	taskID := c.Param("id")

	task, err := h.taskRepo.FindByUUID(taskID)
	if err != nil {
		ErrorNotFound(c, "Task not found")
		return
	}

	// Look for the file in the task output directory
	var filePath string
	switch fileType {
	case "excel":
		filePath = findFileByPattern(task.OutputDir, "*.xlsx")
	case "parquet":
		filePath = findFileByPattern(task.OutputDir, "*.parquet")
	case "vcf":
		filePath = findVCFFile(task.OutputDir, false)
	case "mt-vcf":
		filePath = findVCFFile(task.OutputDir, true)
	}

	if filePath == "" {
		ErrorNotFound(c, fmt.Sprintf("%s file not found", fileType))
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(filePath)))
	c.Header("Content-Type", contentType)
	c.File(filePath)
}

// findFileByPattern finds the first file matching a glob pattern in a directory
func findFileByPattern(dir, pattern string) string {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(matches) == 0 {
		// Try subdirectories
		matches, err = filepath.Glob(filepath.Join(dir, "**", pattern))
		if err != nil || len(matches) == 0 {
			return ""
		}
	}
	return matches[0]
}

// findVCFFile finds VCF files, distinguishing between MT and regular
func findVCFFile(dir string, isMT bool) string {
	pattern := "*.vcf.gz"
	if isMT {
		pattern = "*.mt.vcf.gz"
	}

	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err == nil && len(matches) > 0 {
		return matches[0]
	}

	// Try uncompressed
	pattern = "*.vcf"
	if isMT {
		pattern = "*.mt.vcf"
	}
	matches, err = filepath.Glob(filepath.Join(dir, pattern))
	if err == nil && len(matches) > 0 {
		return matches[0]
	}

	// Try _LAST symlink pattern (Sepiida output structure)
	lastLink := filepath.Join(dir, "_LAST")
	if target, err := os.Readlink(lastLink); err == nil {
		lastDir := filepath.Join(dir, target)
		matches, _ = filepath.Glob(filepath.Join(lastDir, pattern))
		if len(matches) > 0 {
			return matches[0]
		}
	}

	return ""
}
