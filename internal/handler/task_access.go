package handler

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/middleware"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/gin-gonic/gin"
)

// requireTaskAccess verifies that the task exists and the user has access to it.
// Returns the task and true if access is granted, or sends an error response and returns nil, false.
func requireTaskAccess(c *gin.Context, taskRepo *repository.TaskRepository, taskUUID string) (*model.Task, bool) {
	task, err := taskRepo.FindByUUID(taskUUID)
	if err != nil {
		ErrorNotFound(c, "Task not found")
		return nil, false
	}

	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return nil, false
	}

	if role == string(model.SystemRoleSuperAdmin) {
		return task, true
	}

	if orgID, ok := middleware.GetCurrentOrg(c); ok && task.ExternalOrgID != "" && task.ExternalOrgID == orgID {
		return task, true
	}

	if task.CreatedBy != 0 && task.CreatedBy == userID {
		return task, true
	}

	// Return 404 instead of 403 so cross-tenant task UUID probing does not reveal
	// whether the target task exists.
	c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
	c.Abort()
	return nil, false
}

func applyTaskListScope(c *gin.Context, query *model.TaskListQuery) bool {
	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return false
	}
	if role == string(model.SystemRoleSuperAdmin) {
		query.IncludeAll = true
		return true
	}
	if orgID, ok := middleware.GetCurrentOrg(c); ok && orgID != "" {
		query.ExternalOrgID = orgID
		return true
	}
	query.CreatedBy = userID
	return true
}

func applyCreatedByListScope(c *gin.Context, createdBy *uint, includeAll *bool) bool {
	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return false
	}
	if role == string(model.SystemRoleSuperAdmin) {
		*includeAll = true
		return true
	}
	*createdBy = userID
	return true
}

func requireOwnerAccess(c *gin.Context, ownerID uint, resourceName string) bool {
	userID, _, role, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return false
	}
	if role == string(model.SystemRoleSuperAdmin) || (ownerID != 0 && ownerID == userID) {
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": resourceName + " not found"})
	c.Abort()
	return false
}
