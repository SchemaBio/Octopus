package middleware

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/retention"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func ResearchPolicy(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.IsSaaS() {
			segments := strings.Split(strings.Trim(c.Request.URL.Path, "/"), "/")
			for index, part := range segments {
				if part == "history" && (index == 0 || segments[index-1] != "v1") {
					continue
				}
				switch part {
				case "history", "report-templates", "reports", "report-preview", "report-generations":
					c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Feature unavailable in SaaS edition"})
					return
				}
			}
		}
		if cfg.IsSaaS() && strings.Contains(c.FullPath(), "/tasks/:id/") && c.Param("id") != "" {
			ctx, done := retention.Track(c.Request.Context(), c.Param("id"))
			defer done()
			c.Request = c.Request.WithContext(ctx)
			if ctx.Err() != nil {
				c.AbortWithStatus(http.StatusGone)
				return
			}
		}
		c.Next()
	}
}
