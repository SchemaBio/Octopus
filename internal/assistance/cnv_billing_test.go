package assistance

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCNVCommercialBillingLifecycle(t *testing.T) {
	for _, f := range []string{"cnv_gain", "cnv_loss"} {
		t.Run(f, func(t *testing.T) { testCNVCommercialBillingLifecycle(t, f) })
	}
}
func testCNVCommercialBillingLifecycle(t *testing.T, feature string) {
	for _, scenario := range []string{"success", "not_eligible", "invalid", "reserve_failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			s, calls := integrationService(t)
			s.CNVBilling = true
			transport := s.Client.HTTP.Transport
			if transport == nil {
				transport = http.DefaultTransport
			}
			s.Client.HTTP = &http.Client{Transport: acmgBillingTransport(func(r *http.Request) (*http.Response, error) {
				if scenario == "cancelled" && strings.HasSuffix(r.URL.Path, "/result") {
					return &http.Response{StatusCode: 202, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				}
				body := ""
				if r.URL.Path == "/v2/capabilities" {
					body = strings.ReplaceAll(`{"engines":[{"id":"ENGINE_CNV","enabled":false,"status":"disabled","ai_available":true,"billing_available":true}]}`, "ENGINE_CNV", Engines[feature])
				}
				if strings.HasSuffix(r.URL.Path, "/result") {
					raw := map[string]any{"schema_version": "sepilens-evaluation-2.0", "delivery_status": "delivered", "result_kind": "evidence_only", "settlement_eligible": scenario != "not_eligible", "agent_proposal": map[string]any{"ai_assessment": map[string]any{"clinical_summary": "Synthetic ACMG summary", "classification": "VUS", "criteria": []any{map[string]any{"code": "1A", "status": "insufficient_data"}}}}}
					if scenario == "invalid" {
						raw["delivery_status"] = "failed"
					}
					b, _ := json.Marshal(raw)
					body = string(b)
				}
				if body != "" {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
				}
				return transport.RoundTrip(r)
			})}
			actions := []string{}
			overlay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				a := strings.TrimPrefix(r.URL.Path, "/api/v1/overlay/ai/")
				actions = append(actions, a)
				if scenario == "reserve_failure" && a == "reserve" {
					w.WriteHeader(409)
					return
				}
				w.WriteHeader(204)
			}))
			defer overlay.Close()
			s.Overlay = &Client{overlay.URL, "synthetic", overlay.Client()}
			in := Input{Feature: feature, Reference: "hg19", SampleContext: map[string]any{"analysis_mode": "trio"}, Variant: map[string]any{"chromosome": "1", "startPosition": float64(0), "endPosition": float64(100), "coordinateSystem": "0-based-half-open", "copyNumber": float64(0)}}
			ctx := context.Background()
			if _, e := s.Submit(ctx, "org:a", "a", 1, "paid", in); e == nil {
				t.Fatal("fee confirmation bypassed")
			}
			if calls.Load() != 0 {
				t.Fatal("model called before consent")
			}
			in.ConfirmedCharge = true
			r, e := s.Submit(ctx, "org:a", "a", 1, "paid", in)
			if e != nil {
				t.Fatal(e)
			}
			again, e := s.Submit(ctx, "org:a", "a", 1, "paid", in)
			if e != nil || r.ID != again.ID {
				t.Fatal("idempotency lost", e)
			}
			current := *r
			for i := 0; i < 6; i++ {
				s.process(ctx, current)
				current, e = s.Get("org:a", 1, r.ID)
				if e != nil {
					t.Fatal(e)
				}
				if scenario == "cancelled" && i == 0 {
					if e = s.Cancel(current); e != nil {
						t.Fatal(e)
					}
					current, e = s.Get("org:a", 1, r.ID)
					if e != nil {
						t.Fatal(e)
					}
				}
				if terminal(current.State) && current.BillingDone {
					break
				}
			}
			want := "reserve,refund"
			if scenario == "success" {
				want = "reserve,settle"
			}
			if strings.Join(actions, ",") != want || !current.BillingDone {
				t.Fatal(actions, current.State, current.BillingDone)
			}
			before := len(actions)
			s.process(ctx, current)
			if len(actions) != before {
				t.Fatal("duplicate settlement")
			}
			if scenario == "reserve_failure" && calls.Load() != 0 {
				t.Fatal("model invoked after failed reserve")
			}
		})
	}
}
