package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

type localFASTQObservation struct {
	files   [2]os.FileInfo
	checked bool
}

func (o localFASTQObservation) matches(other localFASTQObservation) bool {
	for i, before := range o.files {
		after := other.files[i]
		if before == nil || after == nil || !os.SameFile(before, after) ||
			before.Size() != after.Size() || before.Mode() != after.Mode() ||
			!before.ModTime().Equal(after.ModTime()) {
			return false
		}
	}
	return true
}

func observeLocalFASTQPair(root string, pair []model.DataAsset) localFASTQObservation {
	var observation localFASTQObservation
	for i, asset := range pair {
		path, err := safeLocalUploadPath(root, asset.StorageKey)
		if err != nil || !sameLocalPath(path, asset.StorageKey) {
			return localFASTQObservation{}
		}
		info, err := os.Stat(path)
		if err != nil {
			return localFASTQObservation{}
		}
		observation.files[i] = info
	}
	return observation
}

// StartLocalFASTQWait validates registered pairs once their files have settled.
// Pending rows are durable, so restarting Octopus resumes the wait.
func (s *DataAssetService) StartLocalFASTQWait(ctx context.Context, interval time.Duration) {
	if s.cfg.Edition != "opensource" || s.cfg.Storage.Provider != "local" {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		observations := make(map[string]localFASTQObservation)
		for {
			observations = s.checkLocalFASTQPairs(ctx, observations)
			ticker.Reset(interval)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *DataAssetService) checkLocalFASTQPairs(ctx context.Context, previous map[string]localFASTQObservation) map[string]localFASTQObservation {
	db := database.GetDB().WithContext(ctx)
	var pending []model.DataAsset
	err := db.Where("provider = ? AND source = ? AND status = ? AND local_import_pair_id IS NOT NULL",
		model.UploadProviderLocal, model.DataAssetSourceLocalImport, model.FileStatusPending).
		Order("read_type").Find(&pending).Error
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("load pending local FASTQ pairs: %v", err)
		}
		return previous
	}
	pairs := make(map[string][]model.DataAsset)
	for _, asset := range pending {
		id := *asset.LocalImportPairID
		pairs[id] = append(pairs[id], asset)
	}
	next := make(map[string]localFASTQObservation)
	root := s.cfg.Storage.LocalDir
	for id, pair := range pairs {
		if ctx.Err() != nil {
			break
		}
		if len(pair) != 2 || pair[0].ReadType != model.ReadTypeRead1 || pair[1].ReadType != model.ReadTypeRead2 {
			continue
		}
		current := observeLocalFASTQPair(root, pair)
		if current.files[0] == nil {
			continue
		}
		next[id] = current
		prior := previous[id]
		if !current.matches(prior) {
			continue
		}
		if prior.checked {
			next[id] = prior
			continue
		}
		validated, validationErr := validateLocalPair(ctx, root, pair[0].StorageKey, pair[1].StorageKey)
		if ctx.Err() != nil || !current.matches(observeLocalFASTQPair(root, pair)) {
			delete(next, id)
			continue
		}
		if err := saveLocalFASTQValidation(db, pair, validated, validationErr); err != nil {
			log.Printf("save local FASTQ validation for pair %s: %v", id, err)
			continue
		}
		if validationErr == nil {
			delete(next, id)
		} else {
			// Retry a malformed or incomplete pair only after a file changes.
			current.checked = true
			next[id] = current
		}
	}
	return next
}

func saveLocalFASTQValidation(db *gorm.DB, pair, validated []model.DataAsset, validationErr error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for i, asset := range pair {
			fields := map[string]interface{}{"validation_status": "invalid"}
			if validationErr != nil {
				fields["validation_code"] = validationErr.Error()
			} else {
				fields = map[string]interface{}{
					"status": model.FileStatusCompleted, "validation_status": "valid", "validation_code": "",
					"validation_sha256": validated[i].ValidationSHA256, "validated_at": validated[i].ValidatedAt,
					"file_size": validated[i].FileSize,
				}
			}
			result := tx.Model(&model.DataAsset{}).
				Where("id = ? AND status = ?", asset.ID, model.FileStatusPending).Updates(fields)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("pending asset %d changed during validation", asset.ID)
			}
		}
		return nil
	})
}
