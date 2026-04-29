package handler

import (
	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/gin-gonic/gin"
)

type DashboardHandler struct {
	cfg *config.Config
}

func NewDashboardHandler(cfg *config.Config) *DashboardHandler {
	return &DashboardHandler{cfg: cfg}
}

// GetStats returns dashboard statistics
func (h *DashboardHandler) GetStats(c *gin.Context) {
	db := database.GetDB()

	var totalSamples int64
	db.Model(&model.Sample{}).Count(&totalSamples)

	var pendingTasks int64
	db.Model(&model.Task{}).Where("status IN ?", []string{"queued", "pending"}).Count(&pendingTasks)

	var runningTasks int64
	db.Model(&model.Task{}).Where("status = ?", "running").Count(&runningTasks)

	var completedTasks int64
	db.Model(&model.Task{}).Where("status = ?", "completed").Count(&completedTasks)

	Success(c, model.DashboardStats{
		TotalSamples:   int(totalSamples),
		PendingTasks:   int(pendingTasks),
		RunningTasks:   int(runningTasks),
		CompletedTasks: int(completedTasks),
	})
}
