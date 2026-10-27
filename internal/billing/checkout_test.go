package billing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
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
				if r.Header.Get("Stripe-Version") != StripeVersion {
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
			h := NewHandler(api.Runtime{}, config.Config{AppBaseURL: "https://openlocal.store", StripeBusinessPrice: "price_business", StripeSecondModulePrice: "price_addon"})
			h.Stripe = &Client{HTTP: server.Client(), BaseURL: server.URL, Secret: "sk_test"}
			local := db.BusinessSubscription{BusinessID: uuid.New(), StripeCustomerID: sqlText("cus_test")}
			app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
			app.Post("/checkout", func(c *fiber.Ctx) error {
				result, err := h.checkout(c, local, changeInput{Tier: "business", Modules: tc.modules, Locale: "en"}, "checkout-key")
				if err != nil {
					return err
				}
				return c.JSON(result)
			})
			response, err := app.Test(httptest.NewRequest(http.MethodPost, "/checkout", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status || !reflect.DeepEqual(calls, tc.calls) {
				t.Fatalf("status=%d calls=%v; want status=%d calls=%v", response.StatusCode, calls, tc.status, tc.calls)
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
			if tc.status == 200 {
				var result struct {
					URL string `json:"url"`
				}
				if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result.URL == "" {
					t.Fatalf("missing checkout URL: %v", err)
				}
			}
		})
	}
}
