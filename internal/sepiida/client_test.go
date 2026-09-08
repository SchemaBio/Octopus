package sepiida

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientQueriesExactExecutionAndRejectsFallback(t *testing.T) {
	for _, returnedAgent := range []string{"attempt-current", "attempt-old"} {
		t.Run(returnedAgent, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("uuid") != "task-stable" || r.URL.Query().Get("agent_id") != "attempt-current" {
					t.Error("missing joint execution identity")
				}
				fmt.Fprintf(w, `{"workflow":{"id":"run","uuid":"task-stable","agent_id":%q}}`, returnedAgent)
			}))
			defer server.Close()
			result, err := NewClient(server.URL, "query-key").GetWorkflowByAttempt("task-stable", "attempt-current")
			if returnedAgent == "attempt-old" {
				if err == nil {
					t.Fatal("accepted another attempt")
				}
			} else if err != nil || result.AgentID != returnedAgent {
				t.Fatalf("valid execution rejected: %v", err)
			}
		})
	}
}

func TestClientHealthSendsBearerQueryKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "query-token")
	if err := client.Health(); err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if gotAuth != "Bearer query-token" {
		t.Fatalf("expected bearer query key, got %q", gotAuth)
	}
}

func TestClientRejectsMissingQueryKey(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", " ")
	if _, err := client.doRequest(http.MethodGet, "/api/v1/workflows"); err == nil {
		t.Fatal("expected missing query key error")
	}
}

func TestClientRedactsUpstreamErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("secret-token-from-upstream"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "query-token")
	_, err := client.doRequest(http.MethodGet, "/api/v1/workflows")
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if strings.Contains(err.Error(), "secret-token-from-upstream") {
		t.Fatalf("upstream response body leaked in error: %v", err)
	}
}

func TestJoinURLRejectsUserInfoAndNetworkPathRefs(t *testing.T) {
	if _, err := joinURL("http://user:pass@sepiida.local", "/health"); err == nil {
		t.Fatal("expected userinfo in base URL to be rejected")
	}
	if _, err := joinURL("http://sepiida.local", "//evil.example/health"); err == nil {
		t.Fatal("expected network-path reference to be rejected")
	}
}
