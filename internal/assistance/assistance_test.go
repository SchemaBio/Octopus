package assistance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testCipher(t *testing.T) {
	t.Helper()
	t.Setenv("AI_ASSISTANCE_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("AI_ASSISTANCE_MASTER_KEY_FILE", "")
}
func TestEncryptionRejectsOtherRequestAndTampering(t *testing.T) {
	testCipher(t)
	c, e := newCipher()
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := seal(c, "request-a", map[string]string{"diagnosis": "sensitive diagnosis"})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(encoded, "sensitive") {
		t.Fatal("plaintext leaked")
	}
	var out map[string]string
	if open(c, "request-b", encoded, &out) == nil {
		t.Fatal("other request decrypted payload")
	}
	if open(c, "request-a", encoded, &out) != nil || out["diagnosis"] != "sensitive diagnosis" {
		t.Fatal("roundtrip failed")
	}
}
func TestMappingPreservesCoordinatesAndUnverifiedDraft(t *testing.T) {
	in := Input{Feature: "cnv_loss", Reference: "hg19", Variant: map[string]any{"chromosome": "chr1", "startPosition": float64(101), "endPosition": float64(200), "sample_id": "omit", "gnomadAF": 0.01}, ClinicalContext: Context{SampleType: "blood", DiseaseID: "MONDO:1", Inheritance: "unknown"}, Calculator: json.RawMessage(`{"evidence":[{"code":"1A","reason":"draft","verified":true}],"bam":"https://signed.example"}`)}
	path, p, e := BuildPayload(in, "platform-v1")
	if e != nil {
		t.Fatal(e)
	}
	cnv := p["cnv"].(map[string]any)
	if path != "/v2/evaluations" || cnv["start"] != float64(101) || cnv["coordinate_system"] != "1-based-inclusive" {
		t.Fatal(p)
	}
	ctx := p["clinical_context"].(map[string]any)
	if ctx["variant_origin"] != "unknown" {
		t.Fatal("invented origin")
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "signed.example") || strings.Contains(string(raw), "sample_id") || strings.Contains(string(raw), `\"verified\":true`) {
		t.Fatal("untrusted state forwarded")
	}
	if len(p["evidence_package"].(map[string]any)["sources"].([]any)) != 0 {
		t.Fatal("draft marked verified")
	}
}
func TestHPOPolarityAndDegradedResult(t *testing.T) {
	raw := map[string]any{"execution_mode": "model", "degraded": false, "terms": []any{map[string]any{"hpo": "HP:0001250", "name": "Seizure", "polarity": "present"}, map[string]any{"hpo": "HP:0000365", "name": "Hearing loss", "polarity": "family_history"}, map[string]any{"hpo": "HP:0001250", "polarity": "absent"}}}
	r, b, e := NormalizeResult("hpo", "model", raw)
	if e != nil || !b || len(r.HPOTerms) != 1 || len(r.Warnings) != 2 {
		t.Fatal(r, b, e)
	}
	raw["degraded"] = true
	if _, b, e = NormalizeResult("hpo", "model", raw); e == nil || b {
		t.Fatal("degraded result billable")
	}
}
func TestResultLayersAndFutureSuggestionPolicy(t *testing.T) {
	raw := map[string]any{"delivery_status": "delivered", "result_kind": "evidence_only", "settlement_eligible": false, "agent_proposal": map[string]any{"model": map[string]any{"summary": "Review ClinVar", "evidence": []any{map[string]any{"rule_code": "PP5", "reason": "unverified"}}}}, "rule_engine_result": nil, "reviewer_decision": nil}
	r, b, e := NormalizeResult("acmg", "model", raw)
	if e != nil || b || r.Classification != "" || r.Summary != "Review ClinVar" || r.RuleEngineResult != nil {
		t.Fatal(r, b, e)
	}
	if !suggestionBillingPolicy("evidence_only", true) || suggestionBillingPolicy("evidence_only", false) {
		t.Fatal("evidence-only billing must require a separate future policy")
	}
	raw["result_kind"] = "production"
	raw["settlement_eligible"] = true
	if _, b, _ = NormalizeResult("acmg", "model", raw); !b {
		t.Fatal("valid production suggestion must be eligible")
	}
	raw["result_kind"] = "simulation"
	if _, b, _ = NormalizeResult("acmg", "model", raw); b {
		t.Fatal("simulation billable")
	}
}
func TestClientStructuredErrorsAndSubmission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer server-token" || r.Header.Get("Idempotency-Key") != "request-key" {
			t.Error("missing server credentials or key")
		}
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"code":"ruleset_not_available","message":"do not leak clinical data"}}`))
	}))
	defer server.Close()
	c := Client{server.URL, "server-token", server.Client()}
	_, e := c.Call(context.Background(), "POST", "/v2/evaluations", "request-key", map[string]any{}, nil)
	var upstream *UpstreamError
	if !errors.As(e, &upstream) || upstream.Status != 409 || strings.Contains(e.Error(), "clinical") {
		t.Fatal(e)
	}
}
func integrationService(t *testing.T) (*Service, *atomic.Int32) {
	t.Helper()
	dsn := os.Getenv("AI_ASSISTANCE_TEST_DSN")
	if dsn == "" {
		t.Skip("set AI_ASSISTANCE_TEST_DSN for isolated PostgreSQL integration tests")
	}
	db, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	schema := "ai_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if e = db.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal(e)
	}
	// One connection keeps search_path isolated; each test gets its own schema.
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if e = db.Exec("SET search_path TO " + schema).Error; e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; _ = sqlDB.Close() })
	if e = db.AutoMigrate(&Settings{}, &Request{}, &SettingsEvent{}, &ProfileRecord{}); e != nil {
		t.Fatal(e)
	}
	testCipher(t)
	calls := &atomic.Int32{}
	job := "job-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/readyz":
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/v2/capabilities":
			_, _ = w.Write([]byte(`{"hpo_candidate_extraction_available":true,"engines":[{"id":"acmg_amp_2015","enabled":true},{"id":"svc_v4","enabled":false,"status":"disabled"}]}`))
		case strings.HasPrefix(r.URL.Path, "/v2/providers/"):
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"version":1}`))
		case r.URL.Path == "/v2/hpo/extract":
			calls.Add(1)
			_, _ = w.Write([]byte(`{"execution_mode":"model","degraded":false,"terms":[{"hpo":"HP:0001250","name":"Seizure","polarity":"present"}]}`))
		case r.URL.Path == "/v2/evaluations":
			calls.Add(1)
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]any{"job_id": job, "status": "queued"})
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/result"):
			_, _ = w.Write([]byte(`{"schema_version":"sepilens-evaluation-2.0","delivery_status":"delivered","result_kind":"production","agent_proposal":{"ai_assessment":{"criteria":[]}},"rule_engine_result":{"classification":"VUS"},"reviewer_decision":null}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("AI_ASSISTANCE_BASE_URL", server.URL)
	t.Setenv("AI_ASSISTANCE_TOKEN", "token")
	t.Setenv("AI_ASSISTANCE_TOKEN_FILE", "")
	t.Setenv("AI_ASSISTANCE_BILLING_ENABLED", "false")
	s := New(db, true, "", "")
	if _, e = s.SaveSettings(context.Background(), Draft{Enabled: true, BaseURL: "https://provider.example/v1", APIKey: "secret-key", Model: "model", Protocol: "openai_responses"}); e != nil {
		t.Fatal(e)
	}
	return s, calls
}
func TestPersistentRequestsIdempotencyScopeAndNoCharges(t *testing.T) {
	s, calls := integrationService(t)
	in := Input{Feature: "acmg", Reference: "hg38", Variant: map[string]any{"chromosome": "1", "position": float64(1), "ref": "A", "alt": "G", "gene": "TEST"}, ClinicalContext: Context{DiseaseID: "MONDO:1", Inheritance: "AD", SampleType: "blood", VariantOrigin: "germline_confirmed"}}
	r, e := s.Submit(context.Background(), "org:a", "a", 1, "key", in)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			again, e := s.Submit(context.Background(), "org:a", "a", 1, "key", in)
			if e != nil || again.ID != r.ID {
				t.Error("duplicate request", e)
			}
		}()
	}
	wg.Wait()
	changed := in
	changed.Diagnosis = "different"
	if _, e = s.Submit(context.Background(), "org:a", "a", 1, "key", changed); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.Get("org:b", 1, r.ID); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatal("tenant leak")
	}
	if _, e = s.Get("org:a", 2, r.ID); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatal("user leak")
	}
	s.Tick(context.Background())
	loaded, e := s.Get("org:a", 1, r.ID)
	if e != nil || loaded.State != "completed" || loaded.BillingEnabled || calls.Load() != 1 {
		t.Fatal(loaded, e, calls.Load())
	}
	if strings.Contains(loaded.Ciphertext, "clinical text") || strings.Contains(loaded.ResultCiphertext, "Seizure") {
		t.Fatal("plaintext stored")
	}
	view, e := s.View(loaded)
	if e != nil || view.Result.Summary == "" {
		t.Fatal(view, e)
	}
	s.Tick(context.Background())
	if calls.Load() != 1 {
		t.Fatal("replayed model")
	}
}
func TestConfigKeyPreservationAndDisabledCapability(t *testing.T) {
	s, _ := integrationService(t)
	settings, e := s.Settings(context.Background(), true)
	if e != nil || !settings.Connected || settings.Capabilities["svcv4"].Enabled || !settings.KeyConfigured || settings.BillingEnabled {
		t.Fatal(settings, e)
	}
	if _, e = s.SaveSettings(context.Background(), Draft{Enabled: true, BaseURL: "https://provider.example/v1", Model: "new", Protocol: "openai_chat"}); e != nil {
		t.Fatal(e)
	}
	_, draft, e := s.configuredRecord()
	if e != nil || draft.APIKey != "secret-key" {
		t.Fatal("key not retained")
	}
	if _, e = s.Submit(context.Background(), "org:a", "a", 1, "key", Input{Feature: "svcv4"}); e == nil {
		t.Fatal("disabled engine admitted")
	}
	if _, e = s.SaveSettings(context.Background(), Draft{Enabled: true, ClearKey: true}); e != nil {
		t.Fatal(e)
	}
	settings, _ = s.Settings(context.Background(), true)
	if settings.Configured || settings.KeyConfigured {
		t.Fatal("key not cleared")
	}
}
func TestAsyncWorkerStoresResultAndPinnedPayload(t *testing.T) {
	s, calls := integrationService(t)
	in := Input{Feature: "acmg", TaskID: "task", Reference: "hg38", Variant: map[string]any{"gene": "TEST", "chromosome": "1", "position": float64(100), "ref": "A", "alt": "G"}, ClinicalContext: Context{DiseaseID: "MONDO:1", Inheritance: "AD", SampleType: "blood"}}
	r, e := s.Submit(context.Background(), "org:a", "a", 1, "key", in)
	if e != nil {
		t.Fatal(e)
	}
	var stored StoredInput
	if e = open(s.Cipher, r.ID, r.Ciphertext, &stored); e != nil {
		t.Fatal(e)
	}
	original, _ := json.Marshal(stored.Payload)
	if _, e = s.SaveSettings(context.Background(), Draft{Enabled: true, BaseURL: "https://provider.example/v1", Model: "new"}); e != nil {
		t.Fatal(e)
	}
	s.Tick(context.Background())
	loaded, _ := s.Get("org:a", 1, r.ID)
	view, e := s.View(loaded)
	if e != nil || loaded.State != "completed" || loaded.JobID == "" || view.Result.Model != "model" || calls.Load() != 1 {
		t.Fatal(loaded, view, e)
	}
	var same StoredInput
	_ = open(s.Cipher, r.ID, r.Ciphertext, &same)
	current, _ := json.Marshal(same.Payload)
	if string(original) != string(current) {
		t.Fatal("retry payload changed")
	}
}
