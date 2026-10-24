package assistance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"os"
	"strings"
	"sync"
	"time"
)

var memoryOwner = uuid.NewString()

var hpoMemory = struct {
	sync.Mutex
	texts map[string]string
}{texts: map[string]string{}}

func putHPO(id, text string) bool {
	hpoMemory.Lock()
	defer hpoMemory.Unlock()
	if len(hpoMemory.texts) >= 128 {
		return false
	}
	hpoMemory.texts[id] = text
	return true
}
func getHPO(id string) (string, bool) {
	hpoMemory.Lock()
	defer hpoMemory.Unlock()
	v, ok := hpoMemory.texts[id]
	return v, ok
}
func deleteHPO(id string) { hpoMemory.Lock(); defer hpoMemory.Unlock(); delete(hpoMemory.texts, id) }
func settingsProfile(row Settings) string {
	if row.ProfileID != "" {
		return row.ProfileID
	}
	return fmt.Sprintf("platform-v%d", row.Version)
}

func (s *Service) submissionSnapshotMatches(ctx context.Context, feature string, stored StoredInput) (bool, error) {
	var caps struct {
		Version string `json:"version"`
		Engines []struct {
			ID   string `json:"id"`
			Hash string `json:"ruleset_hash"`
		} `json:"engines"`
	}
	if _, err := s.Client.Call(ctx, "GET", "/v2/capabilities", "", nil, &caps); err != nil {
		return false, err
	}
	if caps.Version != stored.ServiceVersion {
		return false, nil
	}
	for _, engine := range caps.Engines {
		if engine.ID == Engines[feature] {
			return engine.Hash == stored.RulesetHash, nil
		}
	}
	return false, nil
}

// Local SQLite ownership needs an operator-assigned identity that follows the
// volume across restarts. External replicas use the same stable service URL.
func (s *Service) upstreamID() string {
	identity := strings.TrimRight(s.Client.BaseURL, "/")
	if os.Getenv("AI_ASSISTANCE_MANAGED_PROCESS") == "true" {
		node := strings.TrimSpace(os.Getenv("AI_ASSISTANCE_INSTANCE_ID"))
		if node == "" {
			return ""
		}
		identity += ":" + node
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}
func (s *Service) observeHPOCancellation(parent context.Context, id string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var r Request
				if s.DB.WithContext(ctx).Select("cancel_requested").First(&r, "id = ?", id).Error == nil && r.CancelRequested {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(); <-done }
}

// Remove encrypted clinical payloads/results after 30 days. Keep only request
// identity and billing metadata for idempotency and ledger reconciliation.
func (s *Service) purgeExpired(ctx context.Context) {
	_ = s.DB.WithContext(ctx).Model(&Request{}).Where("feature = 'hpo' AND state IN ? AND ciphertext <> ''", []string{"completed", "failed", "cancelled", "uncertain", "expired"}).Update("ciphertext", "").Error
	_ = s.DB.WithContext(ctx).Model(&Request{}).Where("state IN ? AND updated_at < ? AND (billing_enabled = false OR billing_done = true)", []string{"completed", "failed", "cancelled"}, time.Now().Add(-30*24*time.Hour)).Updates(map[string]any{"ciphertext": "", "result_ciphertext": "", "state": "expired"}).Error
	if s.DB.Migrator().HasTable("tasks") {
		_ = s.DB.WithContext(ctx).Model(&Request{}).Where("state IN ? AND task_id <> '' AND NOT EXISTS (SELECT 1 FROM tasks WHERE tasks.uuid = ai_assistance_requests.task_id AND tasks.deleted_at IS NULL) AND (billing_enabled = false OR billing_done = true)", []string{"completed", "failed", "cancelled"}).Updates(map[string]any{"ciphertext": "", "result_ciphertext": "", "state": "expired"}).Error
	}
	var expired []Request
	if s.DB.WithContext(ctx).Where("state = 'expired' AND job_id <> '' AND upstream_deleted = false AND upstream_id = ?", s.upstreamID()).Limit(3).Find(&expired).Error == nil {
		for _, request := range expired {
			if !validJobID(request.JobID) {
				continue
			}
			cleanup, cancel := context.WithTimeout(ctx, 2*time.Second)
			_, err := s.Client.Call(cleanup, "DELETE", "/v2/jobs/"+request.JobID, "", nil, nil)
			cancel()
			var upstream *UpstreamError
			if err == nil || (errors.As(err, &upstream) && upstream.Status == 404) {
				_ = s.DB.Model(&request).Update("upstream_deleted", true).Error
			}
		}
	}
}

func (s *Service) DeleteResult(r Request) error {
	updated := s.DB.Model(&Request{}).Where("id = ? AND state IN ? AND (billing_enabled = false OR billing_done = true)", r.ID, []string{"completed", "failed", "cancelled", "expired"}).Updates(map[string]any{"ciphertext": "", "result_ciphertext": "", "state": "expired"})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return ErrConflict
	}
	return nil
}
