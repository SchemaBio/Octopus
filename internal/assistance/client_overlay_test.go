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
	"testing"
	"time"
)

func TestOverlayBillingSignatureContract(t *testing.T) {
	for _, action := range []string{"reserve", "settle", "refund"} {
		t.Run(action, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				timestamp := r.Header.Get("X-Overlay-Timestamp")
				issued, err := strconv.ParseInt(timestamp, 10, 64)
				mac := hmac.New(sha256.New, []byte("test-secret"))
				mac.Write([]byte(timestamp + "."))
				mac.Write(raw)
				signature, decodeErr := hex.DecodeString(r.Header.Get("X-Overlay-Signature"))
				if err != nil || decodeErr != nil || time.Since(time.Unix(issued, 0)).Abs() > 5*time.Minute || !hmac.Equal(signature, mac.Sum(nil)) || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Error("invalid Squid overlay authentication")
					w.WriteHeader(401)
					return
				}
				var payload map[string]any
				if json.Unmarshal(raw, &payload) != nil || payload["feature"] != "hpo" || payload["request_id"] != "request" || r.URL.Path != "/api/v1/overlay/ai/"+action {
					t.Error("billing payload changed")
					w.WriteHeader(400)
					return
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			s := New(nil, true, server.URL, "test-secret")
			s.Overlay.HTTP = server.Client()
			if err := s.billing(context.Background(), Request{ID: "request", Feature: "hpo", BillingEnabled: true}, action); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSepiLensDoesNotReceiveOverlaySignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Overlay-Signature") != "" || r.Header.Get("X-Overlay-Timestamp") != "" {
			t.Error("overlay signature sent to model service")
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	client := &Client{server.URL, "model-token", server.Client()}
	if _, err := client.Call(context.Background(), "POST", "/v2/hpo/extract", "", map[string]string{"text": "synthetic"}, nil); err != nil {
		t.Fatal(err)
	}
}
