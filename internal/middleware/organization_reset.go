package middleware

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Hold a shared transaction lock for the complete admitted write request.
// Reset preparation drains these requests before the workspace fence is set,
// including requests that have not reached their first database write yet.
func withOrganizationResetGuard(c *gin.Context, cfg *config.Config) {
	org := c.GetString("org_id")
	if !cfg.IsSaaS() || org == "" || c.IsAborted() {
		c.Next()
		return
	}
	db := database.GetDB()
	if db == nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "organization unavailable"})
		return
	}
	query := db.WithContext(c.Request.Context())
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		tx := query.Begin()
		if tx.Error != nil {
			c.AbortWithStatus(503)
			return
		}
		defer tx.Rollback()
		if tx.Exec("SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))", "organization-reset:"+org).Error != nil {
			c.AbortWithStatus(503)
			return
		}
		query = tx
	}
	var n int64
	if err := query.Model(&model.OrganizationReset{}).Where("org_id=? AND state='clearing'", org).Count(&n).Error; err != nil || n > 0 {
		c.AbortWithStatusJSON(503, gin.H{"error": "organization reset in progress"})
		return
	}
	c.Next()
}
