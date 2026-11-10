package billing_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/billing"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
)

func TestCheckoutPromotionCodes(t *testing.T) {
	for _, tc := range []struct {
		name, sessions, subscriptions string
		modules                       []string
		status, expireStatus          int
		calls                         []string
	}{
		{name: "new single module", modules: []string{"appointments"}, status: 200, calls: []string{"create"}},
		{name: "new two modules", modules: []string{"appointments", "preorders"}, status: 200, calls: []string{"create"}},
		{name: "reuse enabled checkout", sessions: `{"data":[{"id":"cs_old","url":"https://checkout.stripe.com/old","allow_promotion_codes":true,"metadata":{"tier":"business","modules":"appointments"}}]}`, modules: []string{"appointments"}, status: 200},
		{name: "replace legacy checkout", sessions: `{"data":[{"id":"cs_old","url":"https://checkout.stripe.com/old","metadata":{"tier":"business","modules":"appointments"}}]}`, modules: []string{"appointments"}, status: 200, calls: []string{"expire", "create"}},
		{name: "expiration fails", sessions: `{"data":[{"id":"cs_old","metadata":{"tier":"business","modules":"appointments"}}]}`, modules: []string{"appointments"}, status: 503, expireStatus: 500, calls: []string{"expire"}},
		{name: "different selection", sessions: `{"data":[{"id":"cs_old","metadata":{"tier":"essential","modules":"appointments"}}]}`, modules: []string{"appointments"}, status: 409},
		{name: "multiple sessions", sessions: `{"data":[{},{}]}`, modules: []string{"appointments"}, status: 409},
		{name: "existing subscriber", subscriptions: `{"data":[{"status":"active"}]}`, modules: []string{"appointments"}, status: 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			var created url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Stripe-Version") != billing.StripeVersion {
					t.Error("missing pinned API version")
				}
				if r.Method == http.MethodGet {
					if r.URL.Query().Get("customer") != "cus_test" {
						t.Error("checkout lookup is not customer-scoped")
					}
					switch r.URL.Path {
					case "/v1/subscriptions":
						if tc.subscriptions != "" {
							_, _ = w.Write([]byte(tc.subscriptions))
							return
						}
					case "/v1/checkout/sessions":
						if r.URL.Query().Get("status") != "open" {
							t.Error("must only consider open sessions")
						}
						if tc.sessions != "" {
							_, _ = w.Write([]byte(tc.sessions))
							return
						}
					default:
						t.Errorf("unexpected lookup: %s", r.URL)
					}
					_, _ = w.Write([]byte(`{"data":[]}`))
					return
				}
				switch r.URL.Path {
				case "/v1/checkout/sessions/cs_old/expire":
					calls = append(calls, "expire")
					if r.Header.Get("Idempotency-Key") != "checkout-key:expire" {
						t.Error("missing expiration idempotency key")
					}
					if tc.expireStatus != 0 {
						w.WriteHeader(tc.expireStatus)
					}
					_, _ = w.Write([]byte(`{"id":"cs_old","status":"expired"}`))
				case "/v1/checkout/sessions":
					calls = append(calls, "create")
					if r.Header.Get("Idempotency-Key") != "checkout-key" {
						t.Error("missing creation idempotency key")
					}
					_ = r.ParseForm()
					created = r.PostForm
					_, _ = w.Write([]byte(`{"url":"https://checkout.stripe.com/new"}`))
				default:
					t.Errorf("unexpected mutation: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			cfg := config.Config{AppBaseURL: "https://openlocal.store", StripeBusinessPrice: "price_business", StripeSecondModulePrice: "price_addon"}
			local := application.Subscription{Subscription: access.Subscription{BusinessID: uuid.New()}, CustomerID: "cus_test"}
			result, err := payment(server, cfg).Checkout(t.Context(), local, application.ChangeInput{Tier: "business", Modules: tc.modules, Locale: "en"}, "checkout-key")
			if tc.status == 200 {
				if err != nil || result.URL == "" {
					t.Fatalf("checkout result=%+v err=%v", result, err)
				}
			} else {
				var domain *application.Error
				wantKind := "conflict"
				if tc.status == 503 {
					wantKind = "unavailable"
				}
				if !errors.As(err, &domain) || domain.Kind != wantKind {
					t.Fatalf("error=%v; want %s", err, wantKind)
				}
			}
			if !reflect.DeepEqual(calls, tc.calls) {
				t.Fatalf("calls=%v; want %v", calls, tc.calls)
			}
			if created != nil {
				for name, want := range map[string]string{
					"allow_promotion_codes": "true", "mode": "subscription", "customer": "cus_test",
					"line_items[0][price]": "price_business", "line_items[0][quantity]": "1",
					"success_url": "https://openlocal.store/en/dashboard/billing?checkout=success",
					"subscription_data[metadata][openlocal_business_id]": local.BusinessID.String(),
				} {
					if created.Get(name) != want {
						t.Errorf("%s=%q; want %q", name, created.Get(name), want)
					}
				}
				if (created.Get("line_items[1][price]") == "price_addon") != (len(tc.modules) == 2) {
					t.Error("second-module composition changed")
				}
			}
		})
	}
}
