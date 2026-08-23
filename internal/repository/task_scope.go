package repository

import "gorm.io/gorm"

// applyTaskActorScope restricts task-backed records to exactly one ownership
// domain. A standalone user may only see tasks that have no external
// organization; CreatedBy must never grant access to tasks owned by a SaaS
// organization.
func applyTaskActorScope(db *gorm.DB, externalOrgID string, createdBy uint, includeAll bool) *gorm.DB {
	if includeAll {
		return db
	}
	if externalOrgID != "" {
		return db.Where("tasks.external_org_id = ?", externalOrgID)
	}
	if createdBy != 0 {
		return db.Where("tasks.external_org_id = '' AND tasks.created_by = ?", createdBy)
	}
	return db.Where("1 = 0")
}
