package handler

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type ArchiveHandler struct {
	archiver   *service.Archiver
	statusMgr  *service.StatusManager
	parquetGen *service.ParquetGenerator
}

func NewArchiveHandler(cfg *config.Config) *ArchiveHandler {
	return &ArchiveHandler{
		archiver:   service.NewArchiver(cfg),
		statusMgr:  service.NewStatusManager(cfg),
		parquetGen: service.NewParquetGenerator(cfg),
	}
}

// ArchiveStatus godoc
// @Summary Get archive status
// @Description Check if a task has been archived
// @Tags archive
// @Produce json
// @Param uuid path string true "Task UUID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /api/v1/archive/{uuid} [get]
func (h *ArchiveHandler) ArchiveStatus(c *gin.Context) {
	uuid := c.Param("uuid")

	// Check if archive directory exists
	archiveDir := h.archiver.GetArchiveDir(uuid)
	if archiveDir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "archive directory not configured"})
		return
	}

	// Check if UUID archive exists
	files, err := h.archiver.ListArchivedFiles(uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"uuid":     uuid,
			"archived": false,
			"error":    err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"uuid":        uuid,
		"archived":    true,
		"archive_dir": archiveDir,
		"files":       files,
	})
}

// QueryOutput godoc
// @Summary Query output by key
// @Description Query an output value by key from archived outputs.json and get the archived file path
// @Tags archive
// @Produce json
// @Param uuid path string true "Task UUID"
// @Param key path string true "Output key (e.g., 'gvcf' or 'outputs.gvcf')"
// @Success 200 {object} service.OutputQueryResult
// @Failure 404 {object} map[string]string
// @Router /api/v1/archive/{uuid}/output/{key} [get]
func (h *ArchiveHandler) QueryOutput(c *gin.Context) {
	uuid := c.Param("uuid")
	key := c.Param("key")

	result, err := h.archiver.QueryOutputByKey(uuid, key)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"uuid":  uuid,
			"key":   key,
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ListOutputKeys godoc
// @Summary List all output keys
// @Description List all available output keys from archived outputs.json
// @Tags archive
// @Produce json
// @Param uuid path string true "Task UUID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /api/v1/archive/{uuid}/outputs [get]
func (h *ArchiveHandler) ListOutputKeys(c *gin.Context) {
	uuid := c.Param("uuid")

	keys, err := h.archiver.ListOutputKeys(uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"uuid":  uuid,
			"error": err.Error(),
		})
		return
	}

	// Also read full outputs.json content
	archiveDir := h.archiver.GetArchiveDir(uuid)
	outputs := map[string]interface{}{}
	if archiveDir != "" {
		outputsPath := archiveDir + "/outputs.json"
		data, err := os.ReadFile(outputsPath)
		if err == nil {
			var parsed map[string]interface{}
			if json.Unmarshal(data, &parsed) == nil {
				outputs = parsed
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"uuid":    uuid,
		"keys":    keys,
		"outputs": outputs,
	})
}

// GetStatus godoc
// @Summary Get row status data
// @Description Get report_status and review_status for all rows in parquet tables
// @Tags archive
// @Produce json
// @Param uuid path string true "Task UUID"
// @Success 200 {object} service.StatusData
// @Failure 404 {object} map[string]string
// @Router /api/v1/archive/{uuid}/status [get]
func (h *ArchiveHandler) GetStatus(c *gin.Context) {
	uuid := c.Param("uuid")

	status, err := h.statusMgr.GetStatus(uuid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"uuid":  uuid,
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, status)
}

// UpdateStatus godoc
// @Summary Update row status
// @Description Update report_status and review_status for specific rows
// @Tags archive
// @Accept json
// @Produce json
// @Param uuid path string true "Task UUID"
// @Param updates body []service.StatusUpdate true "Status updates"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /api/v1/archive/{uuid}/status [put]
func (h *ArchiveHandler) UpdateStatus(c *gin.Context) {
	uuid := c.Param("uuid")

	var updates []service.StatusUpdate
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"uuid":  uuid,
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"uuid":  uuid,
			"error": "no updates provided",
		})
		return
	}

	if err := h.statusMgr.UpdateStatus(uuid, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"uuid":  uuid,
			"error": err.Error(),
		})
		return
	}

	// Return updated status
	status, _ := h.statusMgr.GetStatus(uuid)
	c.JSON(http.StatusOK, gin.H{
		"uuid":    uuid,
		"success": true,
		"status":  status,
	})
}

// GetParquet godoc
// @Summary Get parquet file info
// @Description Get path to combined parquet file for frontend loading
// @Tags archive
// @Produce json
// @Param uuid path string true "Task UUID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /api/v1/archive/{uuid}/parquet [get]
func (h *ArchiveHandler) GetParquet(c *gin.Context) {
	uuid := c.Param("uuid")

	result, err := h.statusMgr.GetParquetData(uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"uuid":  uuid,
			"error": "parquet file not found: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetCombinedData godoc
// @Summary Get combined parquet data with status
// @Description Get parquet schema preview merged with row status data
// @Tags archive
// @Produce json
// @Param uuid path string true "Task UUID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /api/v1/archive/{uuid}/data [get]
func (h *ArchiveHandler) GetCombinedData(c *gin.Context) {
	uuid := c.Param("uuid")

	archiveDir := h.archiver.GetArchiveDir(uuid)
	if archiveDir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "archive directory not configured"})
		return
	}

	result, err := h.statusMgr.GetCombinedData(uuid, h.parquetGen)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"uuid":  uuid,
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}