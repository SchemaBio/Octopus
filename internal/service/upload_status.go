package service

import (
	"errors"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type uploadJobStatusCounts struct {
	Total     int64
	Active    int64
	Completed int64
	Deleted   int64
	Failed    int64
}

// desiredUploadJobStatus is kept separate from the database operation so the
// lifecycle matrix can be tested without requiring a live PostgreSQL server.
// A deleted file is terminal but does not make a paired job successful: once
// no file is active, any incomplete/deleted member leaves the original job
// failed while preserving files that did complete.
func desiredUploadJobStatus(current model.UploadJobStatus, counts uploadJobStatusCounts) model.UploadJobStatus {
	if current == model.UploadJobStatusDeleting || current == model.UploadJobStatusDeleted {
		return current
	}
	if counts.Total == 0 {
		return model.UploadJobStatusFailed
	}
	if counts.Active > 0 {
		return model.UploadJobStatusUploading
	}
	if counts.Completed == counts.Total {
		return model.UploadJobStatusCompleted
	}
	if counts.Deleted == counts.Total {
		return model.UploadJobStatusDeleted
	}
	return model.UploadJobStatusFailed
}

// reconcileUploadJobStatus recalculates a job from its file rows. Callers
// should invoke it inside the same transaction that changes a file; the job
// row is locked here as defense in depth for completion/delete races.
func reconcileUploadJobStatus(tx *gorm.DB, jobID uint) (model.UploadJobStatus, error) {
	if tx == nil {
		return "", errors.New("database is not initialized")
	}
	var job model.UploadJob
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, jobID).Error; err != nil {
		return "", err
	}

	var rows []struct {
		Status model.FileStatus
		Count  int64
	}
	if err := tx.Model(&model.UploadFile{}).
		Select("status, COUNT(*) AS count").
		Where("job_id = ?", jobID).
		Group("status").
		Scan(&rows).Error; err != nil {
		return "", err
	}
	counts := uploadJobStatusCounts{}
	for _, row := range rows {
		counts.Total += row.Count
		switch row.Status {
		case model.FileStatusPending, model.FileStatusUploading:
			counts.Active += row.Count
		case model.FileStatusCompleted:
			counts.Completed += row.Count
		case model.FileStatusDeleted:
			counts.Deleted += row.Count
		case model.FileStatusFailed, model.FileStatusMissing, model.FileStatusDeleting:
			counts.Failed += row.Count
		}
	}

	desired := desiredUploadJobStatus(job.Status, counts)
	if desired != job.Status {
		if err := tx.Model(&job).Update("status", desired).Error; err != nil {
			return "", err
		}
	}
	return desired, nil
}

func reconcileUploadJobStatusInDB(jobID uint) error {
	db := database.GetDB()
	if db == nil {
		return errors.New("database is not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		_, err := reconcileUploadJobStatus(tx, jobID)
		return err
	})
}
