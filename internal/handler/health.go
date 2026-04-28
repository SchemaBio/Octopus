package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthCheck returns the health status of the service
func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "schema-platform",
	})
}