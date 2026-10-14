package assistance

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

// Complete cancellation, billing and remote erasure before dropping local identities.
func (s *Service) EraseTask(ctx context.Context, task string) error {
	var rows []Request
	if err := s.DB.WithContext(ctx).Where("task_id=?", task).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		deleteHPO(r.ID)
		if !terminal(r.State) {
			if err := s.Cancel(r); err != nil {
				return err
			}
			return ErrConflict
		}
		if r.BillingEnabled && !r.BillingDone {
			return ErrConflict
		}
		if r.JobID != "" && !r.UpstreamDeleted {
			if r.UpstreamID != s.upstreamID() {
				return ErrConflict
			}
			if _, err := uuid.Parse(r.JobID); err != nil {
				return err
			}
			_, err := s.Client.Call(ctx, "DELETE", "/v1/jobs/"+r.JobID, "", nil, nil)
			var upstream *UpstreamError
			if err != nil && !(errors.As(err, &upstream) && upstream.Status == 404) {
				return err
			}
			if err := s.DB.Model(&r).Updates(map[string]interface{}{"upstream_deleted": true, "ciphertext": "", "result_ciphertext": ""}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
