package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
)

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		gin.DefaultWriter.Write([]byte(
			"[" + time.Now().Format("2006-01-02 15:04:05") + "] " +
				c.Request.Method + " " + path + " " +
				string(rune(status)) + " " +
				latency.String() + "\n",
		))
	}
}