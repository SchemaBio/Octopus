package handler

import (
	"net/http"

	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
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

	orgID, _ := middleware.GetCurrentOrg(c)
	if taskAccessAllowed(task, userID, role, orgID) {
		return task, true
	}

	// Return 404 instead of 403 so cross-tenant task UUID probing does not reveal
	// whether the target task exists.
	c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
	c.Abort()
	return nil, false
}

// taskAccessAllowed keeps SaaS tenant ownership and standalone ownership
// mutually exclusive. Once a task belongs to an organization, CreatedBy must
// never grant access from another (or missing) organization context.
func taskAccessAllowed(task *model.Task, userID uint, role, orgID string) bool {
	if task == nil {
		return false
	}
	if role == string(model.SystemRoleSuperAdmin) {
		return true
	}
	if task.ExternalOrgID != "" {
		return orgID != "" && task.ExternalOrgID == orgID
	}
	return task.CreatedBy != 0 && task.CreatedBy == userID
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
		query.TenantID = model.TenantIDForIdentity(orgID, 0)
		return true
	}
	query.CreatedBy = userID
	query.TenantID = model.TenantIDForIdentity("", userID)
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
