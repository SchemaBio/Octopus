package handler

import (
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
	return task, true
}
