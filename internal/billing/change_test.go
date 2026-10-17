package billing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestInvoiceExpandableDecoding(t *testing.T) {
	for _, raw := range []string{`"in_test"`, `{"id":"in_test","status":"paid","amount_paid":956}`, `null`} {
		var s subscription
		if err := json.Unmarshal([]byte(`{"latest_invoice":`+raw+`}`), &s); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if raw != "null" && s.LatestInvoice.ID != "in_test" {
			t.Fatalf("lost invoice ID: %+v", s.LatestInvoice)
		}
	}
}

// Reproduce the provider response at the real change-handler boundary. The
// former decoder failed after Stripe had already applied a successful update.
func TestImmediateChangeResponses(t *testing.T) {
	for _, tc := range []struct {
		name, latestInvoice, pendingUpdate, status, paid, deferred string
		pending                                                    bool
	}{
		{"invoice ID", `"in_test"`, `null`, "processing", "", "", false},
		{"paid", `{"id":"in_test","status":"paid","amount_due":956,"amount_paid":956}`, `null`, "settled", "9.56", "", false},
		{"below minimum", `{"id":"in_test","status":"paid","amount_due":0,"amount_paid":0,"starting_balance":0,"ending_balance":956}`, `null`, "settled", "0.00", "9.56", false},
		{"credit partly covers invoice", `{"id":"in_test","status":"paid","amount_due":0,"amount_paid":0,"starting_balance":-500,"ending_balance":456}`, `null`, "settled", "0.00", "4.56", false},
		{"existing debit", `{"id":"in_test","status":"paid","amount_due":0,"amount_paid":0,"starting_balance":100,"ending_balance":1056}`, `null`, "settled", "0.00", "9.56", false},
		{"credit covers invoice", `{"id":"in_test","status":"paid","amount_due":0,"amount_paid":0,"starting_balance":-2000,"ending_balance":-1044}`, `null`, "settled", "0.00", "", false},
		{"requires payment", `{"id":"in_test","status":"open","amount_due":956,"amount_paid":0}`, `{"expires_at":2000000000}`, "pending", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/sub_test" {
					_, _ = w.Write([]byte(`{"id":"sub_test","customer":"cus_test","latest_invoice":"in_old","items":{"data":[{"id":"si_test","current_period_end":2000000000}]}}`))
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/v1/subscriptions/sub_test" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				updates++
				_ = r.ParseForm()
				if r.Form.Get("expand[]") != "latest_invoice" || r.Form.Get("payment_behavior") != "pending_if_incomplete" || r.Form.Get("proration_behavior") != "always_invoice" {
					t.Errorf("unsafe change parameters: %v", r.Form)
				}
				if r.Header.Get("Idempotency-Key") != "change-key" {
					t.Error("missing idempotency key")
				}
				_, _ = w.Write([]byte(`{"id":"sub_test","customer":"cus_test","latest_invoice":` + tc.latestInvoice + `,"pending_update":` + tc.pendingUpdate + `}`))
			}))
			defer server.Close()
			h := NewHandler(api.Runtime{}, config.Config{StripeBusinessPrice: "price_business", StripeSecondModulePrice: "price_addon"})
			h.Stripe = &Client{HTTP: server.Client(), BaseURL: server.URL, Secret: "sk_test"}
			local := db.BusinessSubscription{Tier: "essential", Modules: []string{"appointments"}, StripeSubscriptionID: sqlText("sub_test"), StripeCustomerID: sqlText("cus_test")}
			app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
			app.Post("/change", func(c *fiber.Ctx) error {
				result, err := h.change(c, nil, local, changeInput{Tier: "business", Modules: []string{"appointments", "preorders"}, ProrationDate: time.Now().Unix()}, "change-key", false)
				if err != nil {
					return err
				}
				return c.JSON(result)
			})
			response, err := app.Test(httptest.NewRequest(http.MethodPost, "/change", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var result changeResult
			if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || updates != 1 || result.PaymentStatus != tc.status || result.AmountPaid != tc.paid || result.AmountDeferred != tc.deferred || result.Pending != tc.pending {
				t.Fatalf("status=%d updates=%d result=%+v", response.StatusCode, updates, result)
			}
		})
	}
}
