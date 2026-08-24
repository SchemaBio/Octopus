package repository

import (
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ResultPackageRepository serializes package preparation per task attempt.
type ResultPackageRepository struct {
	*Repository[model.ResultPackage]
}

func NewResultPackageRepository() *ResultPackageRepository {
	return &ResultPackageRepository{Repository: NewRepository[model.ResultPackage]()}
}

func (r *ResultPackageRepository) FindByTaskAttempt(taskUUID, attemptID string) (*model.ResultPackage, error) {
	return r.find("task_uuid = ? AND execution_attempt_id = ?", taskUUID, attemptID)
}

func (r *ResultPackageRepository) find(query string, args ...interface{}) (*model.ResultPackage, error) {
	var item model.ResultPackage
	err := r.db.Where(query, args...).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// Claim locks the unique task/attempt row and decides whether a builder should
// run. A stale builder is reclaimable; active builders are deduplicated.
func (r *ResultPackageRepository) Claim(taskUUID, attemptID, ownerOrg string, owner uint, fingerprint string, staleAfter time.Duration) (*model.ResultPackage, bool, error) {
	if r.db == nil {
		return nil, false, gorm.ErrInvalidDB
	}
	var result model.ResultPackage
	shouldBuild := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_uuid = ? AND execution_attempt_id = ?", taskUUID, attemptID)
		err := query.First(&result).Error
		if err == gorm.ErrRecordNotFound {
			now := time.Now().UTC()
			result = model.ResultPackage{
				ID: uuidString(), TaskUUID: taskUUID, ExecutionAttemptID: attemptID,
				OwnerUserID: owner, ExternalOrgID: ownerOrg, SourceFingerprint: fingerprint,
				Status: model.ResultPackageBuilding, StartedAt: &now,
			}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&result)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected > 0 {
				shouldBuild = true
				return nil
			}
			// Another request inserted the unique task/attempt row while this
			// transaction was waiting on the conflict check. Reload it under the
			// same lock and apply the normal deduplication rules below.
			if err := query.First(&result).Error; err != nil {
				return err
			}
			err = nil
		}
		if err != nil {
			return err
		}
		if result.Status == model.ResultPackageReady && result.SourceFingerprint == fingerprint {
			return nil
		}
		if result.Status == model.ResultPackageBuilding && result.SourceFingerprint == fingerprint && result.UpdatedAt.After(time.Now().UTC().Add(-staleAfter)) {
			return nil
		}
		now := time.Now().UTC()
		result.SourceFingerprint = fingerprint
		result.Status = model.ResultPackageBuilding
		result.Error = ""
		result.SizeBytes = 0
		result.StartedAt = &now
		result.FinishedAt = nil
		shouldBuild = true
		return tx.Save(&result).Error
	})
	return &result, shouldBuild, err
}

func (r *ResultPackageRepository) MarkReady(id, objectKey, fingerprint string, size int64) error {
	now := time.Now().UTC()
	return r.db.Model(&model.ResultPackage{}).Where("id = ? AND source_fingerprint = ? AND status = ?", id, fingerprint, model.ResultPackageBuilding).Updates(map[string]interface{}{
		"object_key": objectKey, "source_fingerprint": fingerprint, "status": model.ResultPackageReady,
		"size_bytes": size, "error": "", "finished_at": &now,
	}).Error
}

func (r *ResultPackageRepository) MarkFailed(id, message string) error {
	now := time.Now().UTC()
	return r.db.Model(&model.ResultPackage{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status": model.ResultPackageFailed, "error": message, "finished_at": &now,
	}).Error
}

func (r *ResultPackageRepository) MarkFailedForFingerprint(id, fingerprint, message string) error {
	now := time.Now().UTC()
	return r.db.Model(&model.ResultPackage{}).Where("id = ? AND source_fingerprint = ? AND status = ?", id, fingerprint, model.ResultPackageBuilding).Updates(map[string]interface{}{
		"status": model.ResultPackageFailed, "error": message, "finished_at": &now,
	}).Error
}

// uuidString is kept local to avoid coupling repository code to request IDs.
func uuidString() string {
	return uuid.New().String()
}
