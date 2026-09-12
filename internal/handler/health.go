package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/gin-gonic/gin"
)

// HealthCheck returns the health status of the service
func HealthCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := database.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"service": "schema-platform",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "schema-platform",
	})
}
