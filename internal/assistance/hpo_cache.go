package assistance

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// The existing encrypted result is reused for the same user's exact phenotype
// input. Only its keyed hash is queried; this never submits a model request.
func (s *Service) CachedHPO(ctx context.Context, scope string, user uint, diagnosis string) (*Result, error) {
	if s.DB == nil || s.Cipher == nil {
		return nil, ErrUnavailable
	}
	var row Request
	err := scopedQuery(s.DB.WithContext(ctx), scope, user).Where("feature = ? AND hash = ? AND state = ? AND result_ciphertext <> ''", "hpo", inputHash(Input{Feature: "hpo", Diagnosis: diagnosis}), "completed").Order("created_at DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	view, err := s.View(row)
	if err != nil {
		return nil, err
	}
	return view.Result, nil
}
