package assistance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
)

var ErrLegacyACMGBinding = errors.New("historical ACMG result requires original immutable variant verification")

// Authorization is checked for the current reviewer before reading archived content.
func (s *Service) CanSaveCalculatorResult(ctx context.Context, scope, task string, user uint, id, feature string) bool {
	if _, err := uuid.Parse(id); err != nil || (feature != "acmg" && feature != "svcv4") || s.DB == nil {
		return false
	}
	var n int64
	return s.DB.WithContext(ctx).Model(&Request{}).Where("id=? AND scope=? AND task_id=? AND user_id=? AND feature=? AND state=? AND cancel_requested=?", id, scope, task, user, feature, "completed", false).Count(&n).Error == nil && n == 1
}

func (s *Service) SavedACMGResult(ctx context.Context, scope, task, row, datasetHash, id string) (map[string]interface{}, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("invalid AI request ID")
	}
	if s.DB == nil || s.Cipher == nil {
		return nil, ErrUnavailable
	}
	var r Request
	if err := s.DB.WithContext(ctx).Where("id=? AND scope=? AND task_id=?", id, scope, task).First(&r).Error; err != nil {
		return nil, fmt.Errorf("AI result unavailable for this task")
	}
	return s.savedACMGResult(r, task, row, datasetHash)
}

func (s *Service) savedACMGResult(r Request, task, row, datasetHash string) (map[string]interface{}, error) {
	if r.TaskID != task || r.Feature != "acmg" || r.State != "completed" || r.CancelRequested {
		return nil, fmt.Errorf("completed ACMG AI result required")
	}
	v, err := s.View(r)
	if err != nil {
		return nil, err
	}
	if v.Result == nil || v.Result.Feature != "acmg" || strings.TrimSpace(v.Result.Summary) == "" || v.Result.ResultKind == "experimental" || v.Result.VariantID != row || v.Result.DatasetHash == "" || v.Result.DatasetHash != datasetHash {
		if v.Result != nil && (v.Result.VariantID == "" || v.Result.DatasetHash == "") {
			return nil, ErrLegacyACMGBinding
		}
		return nil, fmt.Errorf("AI result does not match this immutable variant dataset")
	}
	copy := *v.Result
	copy.ReviewerDecision = nil
	b, err := json.Marshal(copy)
	if err != nil || len(b) > 1<<20 {
		return nil, fmt.Errorf("invalid AI result")
	}
	var saved map[string]interface{}
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, err
	}
	saved["requestId"] = r.ID
	return saved, nil
}

func (s *Service) SavedLegacyACMGResult(ctx context.Context, scope, task, row, hash, id string, expected map[string]interface{}) (map[string]interface{}, error) {
	var r Request
	if err := s.DB.WithContext(ctx).Where("id=? AND scope=? AND task_id=?", id, scope, task).First(&r).Error; err != nil {
		return nil, ErrLegacyACMGBinding
	}
	if r.Feature != "acmg" || r.State != "completed" || !validJobID(r.JobID) || r.UpstreamID == "" || r.UpstreamID != s.upstreamID() {
		return nil, ErrLegacyACMGBinding
	}
	var snapshot struct {
		EvaluationID string `json:"evaluation_id"`
		Annotations  struct {
			EngineID string                 `json:"engine_id"`
			Variant  map[string]interface{} `json:"variant"`
		} `json:"annotations"`
	}
	if _, err := s.Client.Call(ctx, "GET", "/v2/evaluations/"+r.JobID+"/evidence", "", nil, &snapshot); err != nil {
		return nil, ErrLegacyACMGBinding
	}
	if snapshot.EvaluationID != r.JobID || snapshot.Annotations.EngineID != "acmg_amp_2015" || !sameSVCVariant(snapshot.Annotations.Variant, expected) {
		return nil, ErrLegacyACMGBinding
	}
	v, err := s.View(r)
	if err != nil || v.Result == nil {
		return nil, ErrUnavailable
	}
	if v.Result.Feature != "acmg" || r.CancelRequested {
		return nil, ErrLegacyACMGBinding
	}
	copy := *v.Result
	copy.VariantID = row
	copy.ReviewerDecision = nil
	copy.Warnings = append(append([]string{}, copy.Warnings...), "Historical AI record: variant identity verified; original dataset version is unavailable. Review against the current dataset before adopting evidence.")
	b, err := json.Marshal(copy)
	if err != nil {
		return nil, err
	}
	var saved map[string]interface{}
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, err
	}
	saved["requestId"] = r.ID
	saved["historicalBinding"] = true
	return saved, nil
}
