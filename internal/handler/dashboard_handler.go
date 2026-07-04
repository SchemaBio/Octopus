package handler

import (
	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	orgID, hasOrg := middleware.GetCurrentOrg(c)
	isSuperAdmin := role == string(model.SystemRoleSuperAdmin)

	var totalSamples int64
	sampleDashboardScope(db.Model(&model.Sample{}), userID, isSuperAdmin).Count(&totalSamples)

	var pendingTasks int64
	taskDashboardScope(db.Model(&model.Task{}), userID, orgID, hasOrg, isSuperAdmin).
		Where("status IN ?", []string{"queued", "waiting_for_data"}).
		Count(&pendingTasks)

	var waitingDataTasks int64
	taskDashboardScope(db.Model(&model.Task{}), userID, orgID, hasOrg, isSuperAdmin).
		Where("status = ?", "waiting_for_data").
		Count(&waitingDataTasks)

	var runningTasks int64
	taskDashboardScope(db.Model(&model.Task{}), userID, orgID, hasOrg, isSuperAdmin).
		Where("status = ?", "running").
		Count(&runningTasks)

	var completedTasks int64
	taskDashboardScope(db.Model(&model.Task{}), userID, orgID, hasOrg, isSuperAdmin).
		Where("status = ?", "completed").
		Count(&completedTasks)

	var failedTasks int64
	taskDashboardScope(db.Model(&model.Task{}), userID, orgID, hasOrg, isSuperAdmin).
		Where("status = ?", "failed").
		Count(&failedTasks)

	Success(c, model.DashboardStats{
		TotalSamples:     int(totalSamples),
		PendingTasks:     int(pendingTasks),
		WaitingDataTasks: int(waitingDataTasks),
		RunningTasks:     int(runningTasks),
		CompletedTasks:   int(completedTasks),
		FailedTasks:      int(failedTasks),
	})
}

func sampleDashboardScope(db *gorm.DB, userID uint, isSuperAdmin bool) *gorm.DB {
	if isSuperAdmin {
		return db
	}
	return db.Where("created_by = ?", userID)
}

func taskDashboardScope(db *gorm.DB, userID uint, orgID string, hasOrg, isSuperAdmin bool) *gorm.DB {
	if isSuperAdmin {
		return db
	}
	if hasOrg && orgID != "" {
		return db.Where("external_org_id = ?", orgID)
	}
	return db.Where("created_by = ?", userID)
}
