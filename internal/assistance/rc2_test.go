package assistance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRC2SettlementAndExperimentalLayers(t *testing.T) {
	raw := map[string]any{"delivery_status": "delivered", "result_kind": "production", "agent_proposal": map[string]any{"model": map[string]any{"summary": "valid suggestion"}}}
	if _, b, _ := NormalizeResult("acmg", "m", raw); b {
		t.Fatal("missing upstream eligibility must prevent settlement")
	}
	raw["settlement_eligible"] = true
	if _, b, _ := NormalizeResult("acmg", "m", raw); !b {
		t.Fatal("eligible suggestion rejected")
	}
	raw["result_kind"] = "experimental"
	raw["agent_proposal"] = map[string]any{"experimental_result": map[string]any{"version": "locked", "inputs_verified": false, "clinical_use": "forbidden", "total_points": "8", "classification": "likely_pathogenic"}}
	result, bill, err := NormalizeResult("svcv4", "m", raw)
	if err != nil || bill || result.Classification != "" || result.ExperimentalResult == nil {
		t.Fatal(result, bill, err)
	}
}

func TestRC2ExperimentalCapabilityAndExplicitOrigin(t *testing.T) {
	s, _ := integrationService(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			_, _ = w.Write([]byte(`{"hpo_candidate_extraction_available":true,"engines":[{"id":"svc_v4","enabled":false,"status":"experimental","experimental_available":true},{"id":"acmg_amp_2015","enabled":false,"status":"disabled"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	s.Client = &Client{server.URL, "token", server.Client()}
	_ = s.DB.Model(&Settings{}).Where("id = 1").Update("upstream_id", s.upstreamID()).Error
	settings, err := s.Settings(context.Background(), false)
	if err != nil || !settings.Capabilities["svcv4"].Enabled || settings.Capabilities["svcv4"].Mode != "experimental" || settings.Capabilities["acmg"].Enabled {
		t.Fatal(settings, err)
	}
	input := Input{Feature: "svcv4", Mode: "experimental", Reference: "hg38", Variant: map[string]any{"gene": "TEST", "chromosome": "1", "position": float64(1), "ref": "A", "alt": "G"}, ClinicalContext: Context{DiseaseID: "MONDO:1", Inheritance: "AD", SampleType: "blood", VariantOrigin: "unknown"}, DraftAssessment: json.RawMessage(`{"upstream_commit":"ef66faff51a265fef7b5c4e6439905f3aa540c46","moi":"AD"}`)}
	if _, _, err = BuildPayload(input, "profile"); err == nil {
		t.Fatal("unknown origin was silently scored")
	}
	input.ClinicalContext.VariantOrigin = "germline_confirmed"
	_, body, err := BuildPayload(input, "profile")
	if err != nil || body["options"].(map[string]any)["allow_experimental"] != true || body["draft_assessment"] == nil {
		t.Fatal(body, err)
	}
}

func TestHPOOriginalNeverPersistedAndCompletedWinsCancel(t *testing.T) {
	s, _ := integrationService(t)
	request, err := s.Submit(context.Background(), "org:a", "a", 1, "memory-only", Input{Feature: "hpo", Diagnosis: "sensitive clinical original"})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot StoredInput
	if err = open(s.Cipher, request.ID, request.Ciphertext, &snapshot); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "sensitive") || snapshot.Payload["text"] != nil {
		t.Fatal("HPO original persisted")
	}
	if _, ok := getHPO(request.ID); !ok {
		t.Fatal("memory input missing")
	}
	_ = s.Cancel(*request)
	if err = s.finish(*request, "completed", "", &Result{Feature: "hpo", Summary: "delivered"}, true); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Get("org:a", 1, request.ID)
	if current.State != "completed" || current.Ciphertext != "" {
		t.Fatal("cancel overrode delivered result", current.State)
	}
	if _, ok := getHPO(request.ID); ok {
		t.Fatal("original retained after completion")
	}
}

func TestActiveHPOCancellationStopsHTTP(t *testing.T) {
	s, _ := integrationService(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	request, err := s.Submit(context.Background(), "org:a", "a", 1, "active-cancel", Input{Feature: "hpo", Diagnosis: "original"})
	if err != nil {
		t.Fatal(err)
	}
	s.Client = &Client{server.URL, "token", server.Client()}
	request.UpstreamID = s.upstreamID()
	_ = s.DB.Model(request).Update("upstream_id", request.UpstreamID).Error
	done := make(chan struct{})
	go func() { s.process(context.Background(), *request); close(done) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("call not started")
	}
	_ = s.Cancel(*request)
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP context not cancelled")
	}
	<-done
	current, _ := s.Get("org:a", 1, request.ID)
	if current.State != "cancelled" {
		t.Fatal(current.State)
	}
}

func TestOwnerRoutingAndUnknownProbe(t *testing.T) {
	s, _ := integrationService(t)
	request, err := s.Submit(context.Background(), "org:a", "a", 1, "wrong-node", Input{Feature: "hpo", Diagnosis: "original"})
	if err != nil {
		t.Fatal(err)
	}
	request.UpstreamID = "other-instance"
	s.process(context.Background(), *request)
	current, _ := s.Get("org:a", 1, request.ID)
	if current.State != "queued" {
		t.Fatal("wrong instance processed request")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["api_key"] != "secret-key" {
			t.Error("blank key was not completed server-side")
		}
		_, _ = w.Write([]byte(`{"capabilities":{"connectivity":"supported","structured_output":"unknown","streaming":"unsupported"}}`))
	}))
	defer server.Close()
	s.Client = &Client{server.URL, "token", server.Client()}
	states, err := s.TestProvider(context.Background(), Draft{BaseURL: "https://provider.example/v1", Model: "m", Protocol: "openai_chat", Reason: "验证模型能力接口"}, 1, "a")
	if err != nil || states["structured_output"] != "unknown" || states["streaming"] != "unsupported" || calls.Load() != 1 {
		t.Fatal(states, err)
	}
}

func TestExpiredResultAndPinnedProfiles(t *testing.T) {
	s, _ := integrationService(t)
	request, err := s.Submit(context.Background(), "org:a", "a", 1, "expired", Input{Feature: "hpo", Diagnosis: "original"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.finish(*request, "completed", "", &Result{Feature: "hpo", Summary: "old"}, false)
	_ = s.DB.Model(request).Update("updated_at", time.Now().Add(-31*24*time.Hour)).Error
	s.purgeExpired(context.Background())
	current, _ := s.Get("org:a", 1, request.ID)
	if current.State != "expired" || current.ResultCiphertext != "" {
		t.Fatal("retention did not purge")
	}
	var old Settings
	_ = s.DB.First(&old, 1).Error
	_, err = s.SaveSettings(context.Background(), Draft{Enabled: false, ClearKey: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveSettings(context.Background(), Draft{Enabled: true, BaseURL: "https://provider.example/v1", APIKey: "new-key", Model: "m", Protocol: "openai_chat"})
	if err != nil {
		t.Fatal(err)
	}
	var next Settings
	_ = s.DB.First(&next, 1).Error
	if old.ProfileID == next.ProfileID || !strings.HasPrefix(next.ProfileID, "platform-") {
		t.Fatal("profile identity reused")
	}
}

func TestReplayVersionConflictRemainsUncertain(t *testing.T) {
	s, calls := integrationService(t)
	in := Input{Feature: "acmg", Reference: "hg38", Variant: map[string]any{"chromosome": "1", "position": float64(1), "ref": "A", "alt": "G", "gene": "TEST"}, ClinicalContext: Context{DiseaseID: "MONDO:1", Inheritance: "AD", SampleType: "blood"}}
	r, err := s.Submit(context.Background(), "org:a", "a", 1, "lost-response", in)
	if err != nil {
		t.Fatal(err)
	}
	var stored StoredInput
	_ = open(s.Cipher, r.ID, r.Ciphertext, &stored)
	stored.ServiceVersion = "previous-release"
	stored.RulesetHash = "previous-rules"
	cipher, err := seal(s.Cipher, r.ID, stored)
	if err != nil {
		t.Fatal(err)
	}
	r.State = "submitting"
	r.Ciphertext = cipher
	_ = s.DB.Model(r).Updates(map[string]any{"state": r.State, "ciphertext": cipher}).Error
	s.process(context.Background(), *r)
	current, _ := s.Get("org:a", 1, r.ID)
	if current.State != "uncertain" || calls.Load() != 0 {
		t.Fatal("upstream version change was replayed", current.State)
	}
}

func TestResultDeletionReconcilesOnlyOriginalUpstream(t *testing.T) {
	s, _ := integrationService(t)
	in := Input{Feature: "acmg", Reference: "hg38", Variant: map[string]any{"chromosome": "1", "position": float64(1), "ref": "A", "alt": "G", "gene": "TEST"}, ClinicalContext: Context{DiseaseID: "MONDO:1", Inheritance: "AD", SampleType: "blood"}}
	r, err := s.Submit(context.Background(), "org:a", "a", 1, "delete-job", in)
	if err != nil {
		t.Fatal(err)
	}
	s.process(context.Background(), *r)
	current, _ := s.Get("org:a", 1, r.ID)
	if err = s.DeleteResult(current); err != nil {
		t.Fatal(err)
	}
	s.purgeExpired(context.Background())
	current, _ = s.Get("org:a", 1, r.ID)
	if !current.UpstreamDeleted || current.State != "expired" || current.ResultCiphertext != "" {
		t.Fatal("deletion not reconciled")
	}
}
