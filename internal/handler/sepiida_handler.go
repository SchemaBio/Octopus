package handler

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/sepiida"
	"github.com/gin-gonic/gin"
)

type SepiidaHandler struct {
	client *sepiida.Client
}

func NewSepiidaHandler(cfg *config.Config) *SepiidaHandler {
	var client *sepiida.Client
	if cfg.Sepiida.Enabled && cfg.Sepiida.QueryKey != "" {
		client = sepiida.NewClient(cfg.Sepiida.ServerURL, cfg.Sepiida.QueryKey)
	}
	return &SepiidaHandler{client: client}
}

// HealthCheck checks Sepiida server health
func (h *SepiidaHandler) HealthCheck(c *gin.Context) {
	if h.client == nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "disabled",
			"message": "Sepiida integration is not enabled",
		})
		return
	}

	err := h.client.Health()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "unhealthy",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"message": "Sepiida server is reachable",
	})
}

// ListWorkflows lists all workflows from Sepiida
func (h *SepiidaHandler) ListWorkflows(c *gin.Context) {
	if h.client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Sepiida integration is not enabled",
		})
		return
	}

	workflows, err := h.client.ListWorkflows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflows": workflows,
		"total":     len(workflows),
	})
}