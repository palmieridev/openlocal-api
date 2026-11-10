package billing_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/billing"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/config"
)

func TestVerifySignature(t *testing.T) {
	body := `{"id":"evt_ignored","type":"customer.created"}`
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	billing.NewHandler(api.Runtime{}, config.Config{StripeWebhookSecret: "whsec_test"}).RegisterWebhookRoutes(app)
	for _, tc := range []struct {
		name, body, secret string
		at                 time.Time
		status             int
	}{
		{"valid", body, "whsec_test", time.Now(), 200},
		{"wrong secret", body, "wrong", time.Now(), 400},
		{"tampered body", body + " ", "whsec_test", time.Now(), 400},
		{"expired", body, "whsec_test", time.Now().Add(-6 * time.Minute), 400},
		{"future", body, "whsec_test", time.Now().Add(6 * time.Minute), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/webhooks/stripe", strings.NewReader(tc.body))
			req.Header.Set("Stripe-Signature", signature(body, tc.secret, tc.at))
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != tc.status {
				t.Fatalf("status=%d want=%d", res.StatusCode, tc.status)
			}
		})
	}
}

func TestStripeClientPinsVersionAndIdempotency(t *testing.T) {
	business := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/customers" {
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Stripe-Version") != billing.StripeVersion {
			t.Error("missing version")
		}
		if r.Header.Get("Idempotency-Key") != "customer:"+business.String() {
			t.Error("missing business-scoped key")
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "sk_test" || password != "" {
			t.Error("missing auth")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("metadata[openlocal_business_id]") != business.String() || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("incorrect form: %v", r.Form)
		}
		_, _ = io.WriteString(w, `{"id":"cus_created"}`)
	}))
	defer server.Close()
	id, err := payment(server, config.Config{}).CreateCustomer(t.Context(), business)
	if err != nil || id != "cus_created" {
		t.Fatalf("%v %s", err, id)
	}
}

func TestStripeClientMapsFailuresWithoutLeakingProviderBody(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, kind string
	}{
		{"provider failure", 500, "secret provider detail", "unavailable"},
		{"payment attention", 402, "secret payment detail", "conflict"},
		{"malformed response", 200, "secret invalid JSON", "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			_, err := payment(server, config.Config{}).CreateCustomer(t.Context(), uuid.New())
			var domain *application.Error
			if !errors.As(err, &domain) || domain.Kind != tc.kind || domain.Code != "billing_unavailable" || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}

func TestInterpretSubscriptionRequiresKnownMonthlyComposition(t *testing.T) {
	base := map[string]any{"quantity": 1, "price": map[string]any{"id": "price_e", "currency": "mxn", "active": true}}
	addon := map[string]any{"quantity": 1, "price": map[string]any{"id": "price_a", "currency": "mxn", "active": true}}
	unknown := map[string]any{"quantity": 1, "price": map[string]any{"id": "unknown", "currency": "mxn", "active": true}}
	for _, tc := range []struct {
		name    string
		items   []map[string]any
		modules int
		valid   bool
	}{
		{"base", []map[string]any{base}, 1, true},
		{"addon", []map[string]any{base, addon}, 2, true},
		{"unknown price", []map[string]any{base, unknown}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, err := json.Marshal(tc.items)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("customer") != "cus_test" {
					t.Error("subscription lookup is not customer-scoped")
				}
				_, _ = fmt.Fprintf(w, `{"data":[{"id":"sub_test","customer":"cus_test","status":"active","metadata":{"base_module":"appointments"},"items":{"data":%s}}]}`, items)
			}))
			defer server.Close()
			subs, err := payment(server, config.Config{StripeEssentialPrice: "price_e", StripeBusinessPrice: "price_b", StripeSecondModulePrice: "price_a"}).Subscriptions(t.Context(), "cus_test")
			if !tc.valid {
				var domain *application.Error
				if !errors.As(err, &domain) || domain.Code != "billing_unavailable" {
					t.Fatalf("unknown price accepted: %+v %v", subs, err)
				}
				return
			}
			if err != nil || len(subs) != 1 || subs[0].Tier != "essential" || len(subs[0].Modules) != tc.modules || subs[0].Modules[0] != "appointments" {
				t.Fatalf("subscriptions=%+v err=%v", subs, err)
			}
		})
	}
}

func signature(body, secret string, at time.Time) string {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + body))
	return "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
