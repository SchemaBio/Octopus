package repository

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

// FindQCsByTaskScope returns every member QC document for the exact execution
// attempt. The older singular method remains for compatibility with clients
// that only know a single-sample result.
func (r *ResultRepository) FindQCsByTaskScope(taskID, tenantID, attemptID string) ([]model.QCResult, error) {
	var results []model.QCResult
	err := scopedResultQuery(r.db.Model(&model.QCResult{}), taskID, tenantID, attemptID).
		Order("member_role ASC, member_id ASC, id ASC").
		Find(&results).Error
	return results, err
}

// CountWorkspaceResults is intentionally scoped to one tenant/task/attempt so
// re-imports and retries cannot contribute counts to the displayed result set.
func (r *ResultRepository) CountWorkspaceResults(taskID, tenantID, attemptID string) (map[string]model.ResultCount, error) {
	types := []struct {
		name   string
		model  interface{}
		status bool
	}{
		{"snv-indel", &model.SNVIndel{}, true},
		{"cnv-segment", &model.CNVSegment{}, true},
		{"cnv-exon", &model.CNVExon{}, true},
		{"str", &model.STR{}, true},
		{"mei", &model.MEIVariant{}, true},
		{"mt", &model.MitochondrialVariant{}, true},
		{"upd", &model.UPDRegion{}, true},
		{"roh", &model.ROHRegion{}, true},
		{"qc", &model.QCResult{}, false},
	}
	counts := make(map[string]model.ResultCount, len(types))
	for _, item := range types {
		count, err := workspaceResultCount(r.db, item.model, taskID, tenantID, attemptID, item.status)
		if err != nil {
			return nil, err
		}
		counts[item.name] = count
	}
	return counts, nil
}

func workspaceResultCount(db *gorm.DB, value interface{}, taskID, tenantID, attemptID string, hasStatus bool) (model.ResultCount, error) {
	var result model.ResultCount
	if err := scopedResultQuery(db.Model(value), taskID, tenantID, attemptID).Count(&result.Total).Error; err != nil {
		return result, err
	}
	if !hasStatus {
		return result, nil
	}
	if err := scopedResultQuery(db.Model(value), taskID, tenantID, attemptID).Where("reviewed = ?", true).Count(&result.Reviewed).Error; err != nil {
		return result, err
	}
	if err := scopedResultQuery(db.Model(value), taskID, tenantID, attemptID).Where("reported = ?", true).Count(&result.Reported).Error; err != nil {
		return result, err
	}
	return result, nil
}
