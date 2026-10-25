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

type acmgBillingTransport func(*http.Request) (*http.Response, error)

func (f acmgBillingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestACMGCommercialBillingLifecycle(t *testing.T) {
	for _, scenario := range []string{"success", "not_eligible", "invalid", "reserve_failure"} {
		t.Run(scenario, func(t *testing.T) {
			s, calls := integrationService(t)
			s.ACMGBilling = true
			transport := s.Client.HTTP.Transport
			if transport == nil {
				transport = http.DefaultTransport
			}
			s.Client.HTTP = &http.Client{Transport: acmgBillingTransport(func(r *http.Request) (*http.Response, error) {
				body := ""
				if r.URL.Path == "/v2/capabilities" {
					body = `{"engines":[{"id":"acmg_amp_2015","enabled":false,"status":"disabled","ai_available":true,"billing_available":true}]}`
				}
				if strings.HasSuffix(r.URL.Path, "/result") {
					raw := map[string]any{"schema_version": "sepilens-evaluation-2.0", "delivery_status": "delivered", "result_kind": "evidence_only", "settlement_eligible": scenario != "not_eligible", "agent_proposal": map[string]any{"ai_assessment": map[string]any{"clinical_summary": "Synthetic ACMG summary", "classification": "VUS", "criteria": []any{map[string]any{"code": "PM2", "status": "insufficient_data"}}}}}
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
			in := Input{Feature: "acmg", Reference: "hg19", SampleContext: map[string]any{"analysis_mode": "trio"}, Variant: map[string]any{"chromosome": "1", "position": float64(1), "ref": "A", "alt": "G"}}
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
func TestSVCCommercialBillingLifecycle(t *testing.T) {
	for _, scenario := range []string{"success", "not_eligible", "invalid", "reserve_failure"} {
		t.Run(scenario, func(t *testing.T) {
			s, calls := integrationService(t)
			s.SVCBilling = true
			transport := s.Client.HTTP.Transport
			if transport == nil {
				transport = http.DefaultTransport
			}
			s.Client.HTTP = &http.Client{Transport: acmgBillingTransport(func(r *http.Request) (*http.Response, error) {
				body := ""
				if r.URL.Path == "/v2/capabilities" {
					body = `{"engines":[{"id":"svc_v4","enabled":false,"status":"experimental","experimental_available":true,"ai_available":true,"billing_available":true}]}`
				}
				if strings.HasSuffix(r.URL.Path, "/result") {
					raw := map[string]any{"schema_version": "sepilens-evaluation-2.0", "delivery_status": "delivered", "result_kind": "experimental", "settlement_eligible": scenario != "not_eligible", "agent_proposal": map[string]any{"ai_assessment": map[string]any{"clinical_summary": "Synthetic ACMG summary", "classification": "VUS", "criteria": []any{map[string]any{"code": "PFD", "status": "insufficient_data"}}}}}
					raw["agent_proposal"].(map[string]any)["experimental_result"] = map[string]any{"status": "insufficient_data", "clinical_use": "forbidden", "inputs_verified": false}
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
			in := Input{Feature: "svcv4", Mode: "experimental", Reference: "hg19", SampleContext: map[string]any{"analysis_mode": "trio"}, Variant: map[string]any{"chromosome": "1", "position": float64(1), "ref": "A", "alt": "G"}}
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
func TestACMGCommercialPricingIsExplicit(t *testing.T) {
	t.Setenv("AI_ASSISTANCE_BILLING_ENABLED", "false")
	t.Setenv("AI_ASSISTANCE_ACMG_BILLING_ENABLED", "true")
	s := New(nil, true, "", "")
	if !s.billsFeature("acmg") || s.billsFeature("cnv_loss") || s.billsFeature("svcv4") || New(nil, false, "", "").billsFeature("acmg") {
		t.Fatal("pricing scope changed")
	}
}

func TestSVCCommercialPricingIsExplicit(t *testing.T) {
	t.Setenv("AI_ASSISTANCE_BILLING_ENABLED", "false")
	t.Setenv("AI_ASSISTANCE_SVC_BILLING_ENABLED", "true")
	s := New(nil, true, "", "")
	if !s.billsFeature("svcv4") || s.billsFeature("cnv_loss") || New(nil, false, "", "").billsFeature("svcv4") {
		t.Fatal("SVC pricing scope changed")
	}
}
