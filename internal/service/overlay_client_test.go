package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func testOverlayClient(serverURL string, failOpen bool) *OverlayClient {
	return NewOverlayClient(config.OverlayConfig{
		Enabled:           true,
		BaseURL:           serverURL,
		SharedSecret:      "overlay-secret",
		Timeout:           time.Second,
		FailOpen:          failOpen,
		TaskAdmissionPath: "/admit",
		TaskEventPath:     "/events",
	})
}

func TestOverlayClientAdmitTaskAllowsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admit" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer overlay-secret" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var req model.OverlayTaskAdmissionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Action != model.OverlayAdmissionActionStart || req.Task.UUID != "task-1" {
			t.Fatalf("unexpected admission payload: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(model.OverlayTaskAdmissionResponse{Allowed: true})
	}))
	defer server.Close()

	client := testOverlayClient(server.URL, false)
	err := client.AdmitTask(t.Context(), model.OverlayTaskAdmissionRequest{
		Action: model.OverlayAdmissionActionStart,
		Task:   model.OverlayTaskSnapshot{UUID: "task-1"},
	})
	if err != nil {
		t.Fatalf("expected admission to allow request: %v", err)
	}
}

func TestOverlayClientAdmitTaskReturnsDeniedReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(model.OverlayTaskAdmissionResponse{Allowed: false, Reason: "credits exhausted"})
	}))
	defer server.Close()

	client := testOverlayClient(server.URL, false)
	err := client.AdmitTask(t.Context(), model.OverlayTaskAdmissionRequest{Action: model.OverlayAdmissionActionStart})
	if err == nil || !strings.Contains(err.Error(), "credits exhausted") {
		t.Fatalf("expected denied reason in error, got %v", err)
	}
}

func TestOverlayClientAdmitTaskCanFailOpen(t *testing.T) {
	client := testOverlayClient("http://127.0.0.1:1", true)
	err := client.AdmitTask(t.Context(), model.OverlayTaskAdmissionRequest{Action: model.OverlayAdmissionActionStart})
	if err != nil {
		t.Fatalf("expected fail-open admission to ignore transport error: %v", err)
	}
}

func TestOverlayClientEmitTaskEventPostsEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req model.OverlayTaskEventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if req.Event != model.OverlayTaskEventCompleted || req.Task.UUID != "task-1" {
			t.Fatalf("unexpected event payload: %+v", req)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := testOverlayClient(server.URL, false)
	err := client.EmitTaskEvent(t.Context(), model.OverlayTaskEventRequest{
		Event: model.OverlayTaskEventCompleted,
		Task:  model.OverlayTaskSnapshot{UUID: "task-1"},
	})
	if err != nil {
		t.Fatalf("expected event emission to succeed: %v", err)
	}
}

func TestOverlayClientChargesFixedCredits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/overlay/credits/charge" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req model.OverlayCreditChargeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ReferenceID != "baseline-task" || req.Credits != 6 {
			t.Fatalf("unexpected charge request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(model.OverlayCreditResponse{Allowed: true, Balance: 94})
	}))
	defer server.Close()
	client := testOverlayClient(server.URL, false)
	if err := client.ChargeCredits(t.Context(), model.OverlayCreditChargeRequest{ReferenceID: "baseline-task", Credits: 6}); err != nil {
		t.Fatalf("expected fixed credit charge to succeed: %v", err)
	}
}
