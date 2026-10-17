package handler

import (
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
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

	stats, err := loadDashboardStats(db, userID, orgID, hasOrg, isSuperAdmin, middleware.IsSharedInstance(c))
	if err != nil {
		ErrorInternal(c, "Failed to load dashboard statistics")
		return
	}
	Success(c, stats)
}

func loadDashboardStats(db *gorm.DB, userID uint, orgID string, hasOrg, isSuperAdmin bool, shared ...bool) (model.DashboardStats, error) {
	var totalSamples int64
	if err := sampleDashboardScope(db.Model(&model.Sample{}), userID, orgID, hasOrg, isSuperAdmin, shared...).
		Count(&totalSamples).Error; err != nil {
		return model.DashboardStats{}, err
	}

	var tasks struct {
		Pending     int64 `gorm:"column:pending_tasks"`
		WaitingData int64 `gorm:"column:waiting_data_tasks"`
		Running     int64 `gorm:"column:running_tasks"`
		Completed   int64 `gorm:"column:completed_tasks"`
		Failed      int64 `gorm:"column:failed_tasks"`
	}
	err := taskDashboardScope(db.Model(&model.Task{}), userID, orgID, hasOrg, isSuperAdmin, shared...).
		Select(`
			COUNT(*) FILTER (WHERE status IN (?, ?)) AS pending_tasks,
			COUNT(*) FILTER (WHERE status = ?) AS waiting_data_tasks,
			COUNT(*) FILTER (WHERE status = ?) AS running_tasks,
			COUNT(*) FILTER (WHERE status = ?) AS completed_tasks,
			COUNT(*) FILTER (WHERE status = ?) AS failed_tasks`,
			model.TaskStatusQueued, model.TaskStatusWaitingData,
			model.TaskStatusWaitingData, model.TaskStatusRunning,
			model.TaskStatusCompleted, model.TaskStatusFailed).
		Scan(&tasks).Error
	if err != nil {
		return model.DashboardStats{}, err
	}

	return model.DashboardStats{
		TotalSamples:     int(totalSamples),
		PendingTasks:     int(tasks.Pending),
		WaitingDataTasks: int(tasks.WaitingData),
		RunningTasks:     int(tasks.Running),
		CompletedTasks:   int(tasks.Completed),
		FailedTasks:      int(tasks.Failed),
	}, nil
}

func sampleDashboardScope(db *gorm.DB, userID uint, orgID string, hasOrg, isSuperAdmin bool, shared ...bool) *gorm.DB {
	return dashboardOwnershipScope(db, "samples", userID, orgID, hasOrg, isSuperAdmin, shared...)
}

func taskDashboardScope(db *gorm.DB, userID uint, orgID string, hasOrg, isSuperAdmin bool, shared ...bool) *gorm.DB {
	return dashboardOwnershipScope(db, "tasks", userID, orgID, hasOrg, isSuperAdmin, shared...)
}

func dashboardOwnershipScope(db *gorm.DB, table string, userID uint, orgID string, hasOrg, isSuperAdmin bool, shared ...bool) *gorm.DB {
	if len(shared) > 0 && shared[0] {
		return db.Where("COALESCE(" + table + ".external_org_id, '') = ''")
	}
	if isSuperAdmin {
		return db
	}
	if hasOrg && orgID != "" {
		return db.Where(table+".external_org_id = ?", orgID)
	}
	return db.Where(table+".external_org_id = '' AND "+table+".created_by = ?", userID)
}
