package model

import "fmt"

// TenantIDForIdentity returns the canonical, server-derived scope key used by
// task and result rows. Prefixes prevent an organization identifier from
// colliding with a standalone user identifier.
func TenantIDForIdentity(externalOrgID string, userID uint) string {
	if externalOrgID != "" {
		return "org:" + externalOrgID
	}
	if userID != 0 {
		return fmt.Sprintf("user:%d", userID)
	}
	return ""
}

// TenantIDForTask returns the persisted tenant when available and otherwise
// derives the legacy-compatible scope from the task ownership fields.
func TenantIDForTask(task *Task) string {
	if task == nil {
		return ""
	}
	if task.TenantID != "" {
		return task.TenantID
	}
	return TenantIDForIdentity(task.ExternalOrgID, task.CreatedBy)
}
