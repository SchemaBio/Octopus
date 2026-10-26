package assistance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrLegacySVCBinding = errors.New("historical SVC result requires original variant verification")

// SavedSVCResult copies the completed server result, never browser-written scores
// or clinical claims. The caller must already hold edit access to this task.
func (s *Service) SavedSVCResult(ctx context.Context, scope, taskID, rowID, requestID string) (map[string]interface{}, error) {
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, fmt.Errorf("invalid SVC AI request")
	}
	if s.DB == nil || s.Cipher == nil {
		return nil, ErrUnavailable
	}
	var r Request
	if err := s.DB.WithContext(ctx).Where("id=? AND scope=? AND task_id=?", requestID, scope, taskID).First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("SVC AI result does not belong to this task")
		}
		return nil, ErrUnavailable
	}
	return s.savedSVCResult(r, taskID, rowID)
}

func (s *Service) savedSVCResult(r Request, taskID, rowID string) (map[string]interface{}, error) {
	if r.TaskID != taskID || r.Feature != "svcv4" || r.State != "completed" || r.CancelRequested || r.ResultCiphertext == "" {
		return nil, fmt.Errorf("completed SVC AI result is required")
	}
	view, err := s.View(r)
	if err != nil {
		return nil, ErrUnavailable
	}
	result := view.Result
	if result == nil || result.Feature != "svcv4" || result.ResultKind != "experimental" || strings.TrimSpace(result.Summary) == "" {
		return nil, fmt.Errorf("invalid SVC AI result")
	}
	binding := result.VariantID
	if binding == "" && r.Ciphertext != "" {
		var stored StoredInput
		if open(s.Cipher, r.ID, r.Ciphertext, &stored) != nil {
			return nil, ErrUnavailable
		}
		if stored.Input.TaskID == taskID && stored.Input.Feature == "svcv4" {
			binding = text(stored.Input.Variant["id"])
		}
	}
	if binding == "" {
		return nil, ErrLegacySVCBinding
	}
	if binding != rowID {
		return nil, fmt.Errorf("SVC AI result does not belong to this variant")
	}
	// Saving is archival, not reviewer acceptance or a formal classification.
	copy := *result
	copy.Classification = ""
	copy.RuleEngineResult = nil
	copy.ReviewerDecision = nil
	raw, err := json.Marshal(copy)
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("invalid SVC AI result")
	}
	var saved map[string]interface{}
	if err = json.Unmarshal(raw, &saved); err != nil {
		return nil, fmt.Errorf("invalid SVC AI result")
	}
	saved["requestId"] = r.ID
	return saved, nil
}

// Legacy results erased their request input before row binding was retained.
// Recover only by checking the original immutable upstream evidence against the
// currently authorized Parquet row, never against browser-supplied coordinates.
func (s *Service) SavedLegacySVCResult(ctx context.Context, scope, taskID, rowID, id string, expected map[string]interface{}) (map[string]interface{}, error) {
	var r Request
	if err := s.DB.WithContext(ctx).Where("id=? AND scope=? AND task_id=?", id, scope, taskID).First(&r).Error; err != nil {
		return nil, ErrLegacySVCBinding
	}
	if r.State != "completed" || r.Feature != "svcv4" || !validJobID(r.JobID) || r.UpstreamID == "" || r.UpstreamID != s.upstreamID() {
		return nil, ErrLegacySVCBinding
	}
	var snapshot struct {
		EvaluationID string `json:"evaluation_id"`
		Annotations  struct {
			EngineID string                 `json:"engine_id"`
			Variant  map[string]interface{} `json:"variant"`
		} `json:"annotations"`
	}
	if _, err := s.Client.Call(ctx, "GET", "/v2/evaluations/"+r.JobID+"/evidence", "", nil, &snapshot); err != nil {
		return nil, fmt.Errorf("original SVC evidence is unavailable; result was not changed")
	}
	if snapshot.EvaluationID != r.JobID || snapshot.Annotations.EngineID != "svc_v4" || !sameSVCVariant(snapshot.Annotations.Variant, expected) {
		return nil, fmt.Errorf("historical SVC result does not match the selected variant")
	}
	view, err := s.View(r)
	if err != nil || view.Result == nil {
		return nil, ErrUnavailable
	}
	view.Result.VariantID = rowID
	r.ResultCiphertext, err = seal(s.Cipher, r.ID+":result", view.Result)
	if err != nil {
		return nil, ErrUnavailable
	}
	return s.savedSVCResult(r, taskID, rowID)
}
func sameSVCVariant(original, expected map[string]interface{}) bool {
	canonicalAssembly := func(value interface{}) string {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
		switch s {
		case "hg19", "grch37":
			return "grch37"
		case "hg38", "grch38":
			return "grch38"
		}
		return ""
	}
	if a, b := canonicalAssembly(original["assembly"]), canonicalAssembly(expected["assembly"]); a == "" || a != b {
		return false
	}
	for remote, local := range map[string]string{"chrom": "chromosome", "pos": "position", "ref": "ref", "alt": "alt", "gene": "gene", "transcript": "transcript"} {
		a, b := strings.TrimSpace(fmt.Sprint(original[remote])), strings.TrimSpace(fmt.Sprint(expected[local]))
		if remote == "chrom" {
			a = strings.TrimPrefix(a, "chr")
			b = strings.TrimPrefix(b, "chr")
		}
		if a == "" || b == "" || a == "<nil>" || b == "<nil>" || a != b {
			return false
		}
	}
	return true
}
