package assistance

import (
	"context"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrConflict = errors.New("AI request idempotency conflict")
var ErrUnavailable = errors.New("AI service unavailable")

type Service struct {
	DB      *gorm.DB
	Client  *Client
	Cipher  cipher.AEAD
	SaaS    bool
	Billing bool
	Overlay *Client
}

func New(db *gorm.DB, saas bool, overlayURL, overlayToken string) *Service {
	c, _ := newCipher()
	return &Service{DB: db, Client: newClient(), Cipher: c, SaaS: saas, Billing: saas && os.Getenv("AI_ASSISTANCE_BILLING_ENABLED") == "true", Overlay: &Client{overlayURL, overlayToken, &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}}
}
func (s *Service) configuredRecord() (Settings, Draft, error) {
	row := Settings{ID: 1, Enabled: s.SaaS}
	var draft Draft
	if s.DB == nil || s.Cipher == nil || !validServiceURL(s.Client.BaseURL) || s.Client.Token == "" || s.upstreamID() == "" {
		return row, draft, ErrUnavailable
	}
	err := s.DB.First(&row, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = Settings{ID: 1, Enabled: s.SaaS}
		return row, draft, nil
	}
	if err != nil {
		return row, draft, err
	}
	if row.Ciphertext != "" {
		err = open(s.Cipher, "settings", row.Ciphertext, &draft)
	}
	return row, draft, err
}
func (s *Service) Settings(ctx context.Context, admin bool) (PublicSettings, error) {
	row, draft, err := s.configuredRecord()
	out := PublicSettings{Enabled: row.Enabled, Configured: row.Configured, KeyConfigured: draft.APIKey != "", Protocol: draft.Protocol, BillingEnabled: s.Billing, Capabilities: map[string]Capability{}}
	if out.Protocol == "" {
		out.Protocol = "openai_responses"
	}
	for feature := range Engines {
		out.Capabilities[feature] = Capability{Enabled: false, Reason: "AI 服务尚未就绪", Mode: "formal"}
	}
	if err != nil {
		return out, nil
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err = s.Client.Call(probe, "GET", "/readyz", "", nil, nil); err != nil {
		return out, nil
	}
	var caps struct {
		Version string `json:"version"`
		HPO     bool   `json:"hpo_candidate_extraction_available"`
		Engines []struct {
			AIAvailable  *bool  `json:"ai_available"`
			ID           string `json:"id"`
			Enabled      bool   `json:"enabled"`
			Status       string `json:"status"`
			Experimental bool   `json:"experimental_available"`
			RulesetHash  string `json:"ruleset_hash"`
		} `json:"engines"`
	}
	if _, err = s.Client.Call(probe, "GET", "/v2/capabilities", "", nil, &caps); err != nil {
		return out, nil
	}
	out.Connected = true
	out.ServiceVersion = caps.Version
	out.Model = draft.Model
	if out.Configured {
		if row.UpstreamID != "" && row.UpstreamID != s.upstreamID() {
			out.Configured = false
		}
		if _, e := s.Client.Call(probe, "GET", "/v2/providers/"+settingsProfile(row), "", nil, nil); e != nil {
			out.Configured = false
		}
	}
	if admin {
		out.BaseURL = draft.BaseURL
	}
	for feature, engine := range Engines {
		rulesetHash := ""
		mode := "formal"
		if feature == "hpo" {
			mode = "candidate"
		}
		enabled := false
		reason := "该引擎尚未开放"
		for _, e := range caps.Engines {
			if e.ID == engine {
				rulesetHash = e.RulesetHash
				enabled = e.Enabled
				if e.AIAvailable != nil {
					enabled = *e.AIAvailable
				}
				if feature == "svcv4" && e.Status == "experimental" && e.Experimental && (e.AIAvailable == nil || *e.AIAvailable) {
					enabled = true
					mode = "experimental"
				}
				if e.AIAvailable != nil && *e.AIAvailable && !e.Enabled && feature != "svcv4" {
					mode = "candidate"
				}
				if e.Status != "" {
					reason = "引擎状态：" + e.Status
				}
			}
		}
		if feature == "hpo" {
			enabled = caps.HPO
			reason = "HPO AI 提取尚未就绪"
		}
		if !out.Configured {
			enabled = false
			reason = "AI 模型尚未配置"
		}
		if !row.Enabled {
			enabled = false
			reason = "AI 辅助功能未开启"
		}
		if enabled {
			reason = ""
		}
		if enabled && mode == "experimental" {
			reason = "草案实验 · 输入未核验 · 禁止临床用途"
		}
		out.Capabilities[feature] = Capability{Enabled: enabled, Reason: reason, Mode: mode, BillingEnabled: s.Billing && mode == "formal", RulesetHash: rulesetHash}
	}
	return out, nil
}
func (s *Service) SaveSettings(ctx context.Context, draft Draft) (PublicSettings, error) {
	return s.SaveSettingsAs(ctx, draft, 0, "")
}
func (s *Service) SaveSettingsAs(ctx context.Context, draft Draft, user uint, org string) (PublicSettings, error) {
	if s.Cipher == nil || s.DB == nil || s.upstreamID() == "" {
		return PublicSettings{}, ErrUnavailable
	}
	if draft.Protocol == "" {
		draft.Protocol = "openai_responses"
	}
	if draft.Protocol != "openai_responses" && draft.Protocol != "openai_chat" {
		return PublicSettings{}, errors.New("unsupported model protocol")
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Settings{ID: 1, Enabled: s.SaaS}).Error; e != nil {
			return e
		}
		var row Settings
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, 1).Error; e != nil {
			return e
		}
		var old Draft
		if row.Ciphertext != "" {
			if e := open(s.Cipher, "settings", row.Ciphertext, &old); e != nil {
				return e
			}
		}
		if draft.ClearKey {
			draft.APIKey = ""
		} else if draft.APIKey == "" {
			draft.APIKey = old.APIKey
		}
		ready := draft.APIKey != "" && draft.Model != "" && draft.BaseURL != ""
		if draft.Enabled && !ready && !draft.ClearKey {
			return errors.New("model endpoint, key and model required")
		}
		version := row.Version + 1
		profile := "platform-" + uuid.NewString()
		if ready {
			body := map[string]any{"base_url": draft.BaseURL, "api_key": draft.APIKey, "model": draft.Model, "protocol": draft.Protocol}
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if _, e := s.Client.Call(callCtx, "PUT", "/v2/providers/"+profile, "", body, nil); e != nil {
				return errors.New("SepiLens profile synchronization failed")
			}
			if e := tx.Create(&ProfileRecord{Version: version, ProfileID: profile, UpstreamID: s.upstreamID()}).Error; e != nil {
				return e
			}
		}
		reason := draft.Reason
		draft.ClearKey = false
		draft.Reason = ""
		encoded, e := seal(s.Cipher, "settings", draft)
		if e != nil {
			return e
		}
		if e := tx.Model(&row).Updates(map[string]any{"enabled": draft.Enabled, "configured": ready, "ciphertext": encoded, "version": version, "profile_id": profile, "upstream_id": s.upstreamID()}).Error; e != nil {
			return e
		}
		return tx.Create(&SettingsEvent{UserID: user, OrgID: org, Reason: reason}).Error
	})
	if err != nil {
		return PublicSettings{}, err
	}
	return s.Settings(ctx, true)
}
func scopedQuery(db *gorm.DB, scope string, user uint) *gorm.DB {
	return db.Where("scope = ? AND user_id = ?", scope, user)
}
func (s *Service) Existing(scope string, user uint, key string, in Input) (*Request, error) {
	sum := sha256.Sum256([]byte(key))
	key = hex.EncodeToString(sum[:])
	var r Request
	err := scopedQuery(s.DB, scope, user).Where("key = ?", key).First(&r).Error
	if err != nil {
		return nil, err
	}
	hash := inputHash(in)
	if r.Hash != hash {
		return nil, ErrConflict
	}
	return &r, nil
}
func inputHash(in Input) string {
	in.ConfirmedCharge = false
	b, _ := json.Marshal(in)
	h := hmac.New(sha256.New, []byte(readSecret("AI_ASSISTANCE_MASTER_KEY")))
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Service) Submit(ctx context.Context, scope, org string, user uint, key string, in Input) (*Request, error) {
	if s.DB == nil || s.Cipher == nil {
		return nil, ErrUnavailable
	}
	if key == "" || len(key) > 200 {
		return nil, errors.New("Idempotency-Key required")
	}
	old, err := s.Existing(scope, user, key, in)
	if err == nil {
		return old, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	settings, err := s.Settings(ctx, false)
	if err != nil || !settings.Connected || !settings.Configured || !settings.Enabled {
		return nil, ErrUnavailable
	}
	if cap, ok := settings.Capabilities[in.Feature]; !ok || !cap.Enabled {
		return nil, errors.New("AI engine unavailable")
	}
	if s.upstreamID() == "" {
		return nil, ErrUnavailable
	}
	if in.Feature == "svcv4" && in.Mode != "experimental" {
		return nil, errors.New("SVC requires explicit experimental mode")
	}
	if s.Billing && in.Feature != "svcv4" && in.Feature != "hpo" && !in.ConfirmedCharge {
		return nil, errors.New("credit charge confirmation required")
	}
	var memoryID string
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Settings
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, 1).Error; e != nil {
			return e
		}
		if !row.Enabled || !row.Configured {
			return ErrUnavailable
		}
		var draft Draft
		if e := open(s.Cipher, "settings", row.Ciphertext, &draft); e != nil {
			return e
		}
		if row.UpstreamID != "" && row.UpstreamID != s.upstreamID() {
			return ErrUnavailable
		}
		profile := settingsProfile(row)
		path, payload, e := BuildPayload(in, profile)
		if e != nil {
			return e
		}
		id := uuid.NewString()
		storedInput := in
		if in.Feature == "hpo" {
			if !putHPO(id, in.Diagnosis) {
				return errors.New("HPO queue is full")
			}
			memoryID = id
			storedInput = Input{Feature: "hpo"}
			delete(payload, "text")
		}
		encoded, e := seal(s.Cipher, id, StoredInput{Input: storedInput, ProfileID: profile, ProfileVersion: 1, Path: path, Payload: payload, Model: draft.Model, ServiceVersion: settings.ServiceVersion, RulesetHash: settings.Capabilities[in.Feature].RulesetHash})
		if e != nil {
			return e
		}
		sum := sha256.Sum256([]byte(key))
		r := Request{ID: id, Scope: scope, OrgID: org, UserID: user, Key: hex.EncodeToString(sum[:]), Hash: inputHash(in), TaskID: in.TaskID, Feature: in.Feature, State: "queued", Ciphertext: encoded, BillingEnabled: s.Billing && in.Feature != "svcv4" && in.Feature != "hpo", ProfileVersion: row.Version, UpstreamID: s.upstreamID()}
		if in.Feature == "hpo" {
			r.ExecutionOwner = memoryOwner
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&r).Error
	})
	if err != nil {
		deleteHPO(memoryID)
		return nil, err
	}
	request, err := s.Existing(scope, user, key, in)
	if err != nil || request.ID != memoryID {
		deleteHPO(memoryID)
	}
	return request, err
}
func (s *Service) Get(scope string, user uint, id string) (Request, error) {
	var r Request
	if s.DB == nil {
		return r, ErrUnavailable
	}
	e := scopedQuery(s.DB, scope, user).Where("id = ?", id).First(&r).Error
	return r, e
}
func (s *Service) View(r Request) (View, error) {
	v := View{ID: r.ID, Status: r.State, ErrorCode: r.ErrorCode}
	if r.ResultCiphertext != "" {
		var result Result
		if s.Cipher == nil {
			return v, ErrUnavailable
		}
		if e := open(s.Cipher, r.ID+":result", r.ResultCiphertext, &result); e != nil {
			return v, e
		}
		v.Result = &result
	}
	return v, nil
}
func terminal(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled" || state == "uncertain" || state == "expired"
}
func (s *Service) Cancel(r Request) error {
	return s.DB.Model(&Request{}).Where("id = ? AND state NOT IN ?", r.ID, []string{"completed", "failed", "cancelled", "uncertain"}).Updates(map[string]any{"cancel_requested": true}).Error
}
func (s *Service) billing(ctx context.Context, r Request, action string) error {
	if !r.BillingEnabled {
		return nil
	}
	if s.Overlay.BaseURL == "" || s.Overlay.Token == "" {
		return errors.New("billing unavailable")
	}
	_, err := s.Overlay.Call(ctx, "POST", "/api/v1/overlay/ai/"+action, "", map[string]any{"request_id": r.ID, "org_id": r.OrgID, "user_id": r.UserID, "task_id": r.TaskID, "feature": r.Feature}, nil)
	return err
}
func (s *Service) Start(ctx context.Context) func() {
	done := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				s.Tick(ctx)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	go func() { workers.Wait(); close(done) }()
	return func() { <-done }
}
func (s *Service) Tick(ctx context.Context) {
	if s.DB == nil || s.Cipher == nil {
		return
	}
	s.retireProfiles(ctx)
	s.purgeExpired(ctx)
	var requests []Request
	if s.DB.WithContext(ctx).Where("(state IN ? OR (billing_enabled = ? AND billing_done = ?)) AND lease_until < ? AND (upstream_id = ? OR upstream_id = '') AND (feature <> 'hpo' OR execution_owner = ? OR created_at < ? OR state NOT IN ?)", []string{"queued", "submitting", "running", "hpo_running"}, true, false, time.Now(), s.upstreamID(), memoryOwner, time.Now().Add(-7*time.Minute), []string{"queued", "hpo_running"}).Order("created_at").Limit(20).Find(&requests).Error != nil {
		return
	}
	for _, r := range requests {
		if ctx.Err() != nil {
			return
		}
		token := time.Now().Add(7 * time.Minute)
		claim := s.DB.Model(&Request{}).Where("id = ? AND lease_until < ?", r.ID, time.Now()).Update("lease_until", token)
		if claim.Error != nil || claim.RowsAffected == 0 {
			continue
		}
		local := *s
		local.DB = s.DB.WithContext(ctx)
		local.process(ctx, r)
		_ = s.DB.Model(&Request{}).Where("id = ? AND lease_until = ?", r.ID, token).Update("lease_until", time.Now().Add(2*time.Second)).Error
	}
}

// Retain a superseded secret only while a pinned request needs it. Clearing
// the key disables new requests immediately; old profile keys are removed
// after their in-flight requests finish. Historical result reads need no key.
func (s *Service) retireProfiles(ctx context.Context) {
	_ = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings Settings
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, 1).Error; e != nil {
			return e
		}
		var profiles []ProfileRecord
		if e := tx.Where("deleted = ?", false).Find(&profiles).Error; e != nil {
			return e
		}
		for _, profile := range profiles {
			if profile.UpstreamID != "" && profile.UpstreamID != s.upstreamID() {
				continue
			}
			if settings.Configured && settings.Version == profile.Version {
				continue
			}
			var count int64
			if e := tx.Model(&Request{}).Where("profile_version = ? AND state IN ?", profile.Version, []string{"queued", "submitting", "hpo_running", "running", "uncertain"}).Count(&count).Error; e != nil {
				return e
			}
			if count != 0 {
				continue
			}
			id := profile.ProfileID
			if id == "" {
				id = fmt.Sprintf("platform-v%d", profile.Version)
			}
			_, e := s.Client.Call(ctx, "DELETE", "/v2/providers/"+id, "", nil, nil)
			var upstream *UpstreamError
			if e != nil && !(errors.As(e, &upstream) && upstream.Status == 404) {
				return e
			}
			if e = tx.Model(&profile).Update("deleted", true).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Service) finish(r Request, state, code string, result *Result, billable bool) error {
	var encoded string
	var err error
	if result != nil {
		encoded, err = seal(s.Cipher, r.ID+":result", result)
		if err != nil {
			return err
		}
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var current Request
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", r.ID).Error; e != nil {
			return e
		}
		if terminal(current.State) {
			return nil
		}
		deleteHPO(r.ID)
		return tx.Model(&current).Updates(map[string]any{"state": state, "error_code": code, "result_ciphertext": encoded, "billable": billable, "ciphertext": ""}).Error
	})
}
func (s *Service) process(ctx context.Context, r Request) {
	if r.UpstreamID != "" && r.UpstreamID != s.upstreamID() {
		return
	}
	if r.UpstreamID == "" && r.JobID != "" {
		_ = s.finish(r, "uncertain", "legacy_upstream_unknown", nil, false)
		return
	}
	if !terminal(r.State) && r.TaskID != "" && s.DB.Migrator().HasTable("tasks") {
		var count int64
		if s.DB.Table("tasks").Where("uuid = ? AND deleted_at IS NULL", r.TaskID).Count(&count).Error != nil {
			return
		}
		if count == 0 {
			if s.Cancel(r) != nil {
				return
			}
			r.CancelRequested = true
		}
	}
	if terminal(r.State) {
		if r.BillingEnabled && !r.BillingDone {
			action := "refund"
			if r.State == "completed" && r.Billable {
				action = "settle"
			}
			if s.billing(ctx, r, action) == nil {
				_ = s.DB.Model(&r).Update("billing_done", true).Error
			}
		}
		return
	}
	if r.CancelRequested && r.JobID == "" && r.State == "queued" {
		_ = s.finish(r, "cancelled", "", nil, false)
		return
	}
	if !r.BillingReserved && r.BillingEnabled {
		if err := s.billing(ctx, r, "reserve"); err != nil {
			_ = s.finish(r, "failed", "billing_unavailable", nil, false)
			return
		}
		if s.DB.Model(&r).Update("billing_reserved", true).Error != nil {
			return
		}
		r.BillingReserved = true
	}
	var stored StoredInput
	if open(s.Cipher, r.ID, r.Ciphertext, &stored) != nil {
		_ = s.finish(r, "failed", "encrypted_input_unavailable", nil, false)
		return
	}
	var err error
	// A synchronous HPO call cannot safely be repeated after worker loss.
	if r.State == "hpo_running" {
		_ = s.finish(r, "uncertain", "upstream_outcome_unknown", nil, false)
		return
	}
	if r.JobID == "" {
		if r.Feature != "hpo" && stored.ServiceVersion != "" {
			match, e := s.submissionSnapshotMatches(ctx, r.Feature, stored)
			if e != nil {
				return
			}
			if !match {
				state := "failed"
				if r.State == "submitting" {
					state = "uncertain"
				}
				_ = s.finish(r, state, "upstream_version_changed", nil, false)
				return
			}
		}
		path, payload := stored.Path, stored.Payload
		if path == "" || payload == nil {
			_ = s.finish(r, "failed", "invalid_input", nil, false)
			return
		}
		next := "submitting"
		if r.Feature == "hpo" {
			next = "hpo_running"
		}
		if s.DB.Model(&r).Update("state", next).Error != nil {
			return
		}
		var raw map[string]any
		callCtx := ctx
		stop := func() {}
		if r.Feature == "hpo" {
			text, ok := getHPO(r.ID)
			if !ok {
				_ = s.finish(r, "uncertain", "hpo_input_not_retained", nil, false)
				return
			}
			payload["text"] = text
			callCtx, stop = s.observeHPOCancellation(ctx, r.ID)
		}
		submissionStatus, e := s.Client.Call(callCtx, "POST", path, r.ID, payload, &raw)
		stop()
		if e != nil {
			var upstream *UpstreamError
			var current Request
			_ = s.DB.First(&current, "id = ?", r.ID).Error
			if r.Feature == "hpo" && current.CancelRequested {
				_ = s.finish(r, "cancelled", "", nil, false)
			} else if r.Feature == "hpo" && !errors.As(e, &upstream) {
				_ = s.finish(r, "uncertain", "upstream_outcome_unknown", nil, false)
			} else if errors.As(e, &upstream) {
				state := "failed"
				if upstream.Status == 409 && r.State == "submitting" {
					state = "uncertain"
				}
				_ = s.finish(r, state, upstream.Code, nil, false)
			}
			// Async submission transport errors retain submitting and reuse its key.
			return
		}
		if r.Feature == "hpo" {
			result, bill, e := NormalizeResult(r.Feature, stored.Model, raw)
			if e != nil {
				_ = s.finish(r, "failed", "invalid_result", nil, false)
			} else {
				_ = s.finish(r, "completed", "", &result, bill)
			}
			return
		}
		if submissionStatus != http.StatusAccepted {
			_ = s.finish(r, "failed", "unexpected_sync_evaluation", nil, false)
			return
		}
		id := text(raw["job_id"])
		if id == "" {
			_ = s.finish(r, "uncertain", "invalid_submission_response", nil, false)
			return
		}
		if s.DB.Model(&r).Updates(map[string]any{"state": "running", "job_id": id}).Error != nil {
			return
		}
		r.JobID = id
	}
	var latest Request
	if s.DB.First(&latest, "id = ?", r.ID).Error == nil {
		r.CancelRequested = latest.CancelRequested
	}
	if !validJobID(r.JobID) {
		_ = s.finish(r, "failed", "invalid_job_id", nil, false)
		return
	}
	path := "/v2/jobs/" + r.JobID
	if r.CancelRequested {
		_, err = s.Client.Call(ctx, "POST", path+"/cancel", "", map[string]any{}, nil)
		if err == nil {
			_ = s.finish(r, "cancelled", "", nil, false)
			return
		}
	}
	var raw map[string]any
	status, e := s.Client.Call(ctx, "GET", path+"/result", "", nil, &raw)
	if e != nil {
		var up *UpstreamError
		if errors.As(e, &up) {
			if up.Status == 410 {
				_ = s.finish(r, "cancelled", up.Code, nil, false)
			} else if up.Status == 404 {
				_ = s.DB.Model(&r).Update("error_code", "upstream_job_not_visible").Error
			} else if up.Status == 409 || up.Status == 422 {
				_ = s.finish(r, "failed", up.Code, nil, false)
			}
		}
		return
	}
	if status == http.StatusAccepted {
		return
	}
	if text(raw["schema_version"]) != "sepilens-evaluation-2.0" {
		_ = s.finish(r, "failed", "invalid_result", nil, false)
		return
	}
	result, bill, e := NormalizeResult(r.Feature, stored.Model, raw)
	if e != nil {
		_ = s.finish(r, "failed", "invalid_result", nil, false)
	} else {
		_ = s.finish(r, "completed", "", &result, bill)
	}
}

func ValidateReason(reason string) bool { return len([]rune(strings.TrimSpace(reason))) <= 500 }
