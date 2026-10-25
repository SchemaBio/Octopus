package assistance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHPOProductionBillingDefaultIsSaaSOnly(t *testing.T) {
	t.Setenv("AI_ASSISTANCE_BILLING_ENABLED", "false")
	if !New(nil, true, "", "").billsFeature("hpo") {
		t.Fatal("SaaS HPO must cost one credit")
	}
	if New(nil, false, "", "").billsFeature("hpo") {
		t.Fatal("self-hosted HPO must not charge platform credits")
	}
	if New(nil, true, "", "").billsFeature("acmg") || New(nil, true, "", "").billsFeature("svcv4") {
		t.Fatal("unrelated AI billing enabled")
	}
}

func TestHPOProductionBillingLifecycle(t *testing.T) {
	for _, scenario := range []string{"success", "provider_failure", "degraded", "cancelled", "billing_failure"} {
		t.Run(scenario, func(t *testing.T) {
			started := make(chan struct{})
			var once sync.Once
			s, calls := integrationServiceWithHPO(t, func(w http.ResponseWriter, r *http.Request) {
				if scenario == "provider_failure" {
					w.WriteHeader(503)
					_, _ = w.Write([]byte(`{"error":{"code":"provider_unavailable"}}`))
					return
				}
				if scenario == "cancelled" {
					once.Do(func() { close(started) })
					select {
					case <-r.Context().Done():
					case <-time.After(10 * time.Second):
					}
					return
				}
				degraded := scenario == "degraded"
				_ = json.NewEncoder(w).Encode(map[string]any{"execution_mode": "model", "degraded": degraded, "terms": []any{map[string]any{"hpo": "HP:0001250", "polarity": "present"}}})
			})
			s.HPOBilling = true
			var mu sync.Mutex
			actions := []string{}
			overlay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				timestamp := r.Header.Get("X-Overlay-Timestamp")
				issued, parseErr := strconv.ParseInt(timestamp, 10, 64)
				mac := hmac.New(sha256.New, []byte("isolated-overlay"))
				mac.Write([]byte(timestamp + "."))
				mac.Write(raw)
				signature, decodeErr := hex.DecodeString(r.Header.Get("X-Overlay-Signature"))
				if err != nil || parseErr != nil || decodeErr != nil || time.Since(time.Unix(issued, 0)).Abs() > 5*time.Minute || !hmac.Equal(signature, mac.Sum(nil)) {
					t.Error("billing request rejected by Squid signature contract")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				var payload map[string]any
				_ = json.Unmarshal(raw, &payload)
				if r.Header.Get("Authorization") != "Bearer isolated-overlay" || payload["feature"] != "hpo" || payload["task_id"] != "" {
					t.Error("invalid HPO billing identity")
				}
				action := strings.TrimPrefix(r.URL.Path, "/api/v1/overlay/ai/")
				mu.Lock()
				actions = append(actions, action)
				mu.Unlock()
				if action == "reserve" && scenario == "billing_failure" {
					w.WriteHeader(409)
					return
				}
				w.WriteHeader(204)
			}))
			defer overlay.Close()
			s.Overlay = &Client{overlay.URL, "isolated-overlay", overlay.Client()}
			ctx := context.Background()
			settings, err := s.Settings(ctx, false)
			if err != nil || !settings.Capabilities["hpo"].BillingEnabled || settings.BillingEnabled {
				t.Fatalf("HPO pricing metadata: %+v %v", settings, err)
			}
			input := Input{Feature: "hpo", Diagnosis: "synthetic text"}
			if _, err := s.Submit(ctx, "org:a", "a", 1, "request", input); err == nil {
				t.Fatal("confirmation was not required")
			}
			if calls.Load() != 0 {
				t.Fatal("model invoked without confirmation")
			}
			input.ConfirmedCharge = true
			request, err := s.Submit(ctx, "org:a", "a", 1, "request", input)
			if err != nil || !request.BillingEnabled {
				t.Fatal(request, err)
			}
			again, err := s.Submit(ctx, "org:a", "a", 1, "request", input)
			if err != nil || again.ID != request.ID {
				t.Fatal("duplicate request", err)
			}
			if scenario == "cancelled" {
				done := make(chan struct{})
				go func() { defer close(done); s.process(ctx, *request) }()
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("model did not start")
				}
				if err := s.Cancel(*request); err != nil {
					t.Fatal(err)
				}
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("cancel did not stop HPO")
				}
			} else {
				s.process(ctx, *request)
			}
			current, err := s.Get("org:a", 1, request.ID)
			if err != nil {
				t.Fatal(err)
			}
			s.process(ctx, current) // settle/refund the terminal request
			current, err = s.Get("org:a", 1, request.ID)
			if err != nil || !current.BillingDone {
				t.Fatal("billing unfinished", current, err)
			}
			s.process(ctx, current)
			mu.Lock()
			got := strings.Join(actions, ",")
			mu.Unlock()
			want := "reserve,refund"
			if scenario == "success" {
				want = "reserve,settle"
			}
			if got != want {
				t.Fatalf("actions %s != %s", got, want)
			}
			if scenario == "billing_failure" {
				if calls.Load() != 0 {
					t.Fatal("model called after reserve rejected")
				}
			} else if calls.Load() != 1 {
				t.Fatal("model call duplicated", calls.Load())
			}
			if scenario == "success" {
				result, err := s.CachedHPO(ctx, "org:a", 1, input.Diagnosis)
				if err != nil || result == nil || calls.Load() != 1 {
					t.Fatal("cache read regenerated", err)
				}
				mu.Lock()
				unchanged := strings.Join(actions, ",")
				mu.Unlock()
				if unchanged != want {
					t.Fatal("cache read billed again")
				}
			}
		})
	}
}
