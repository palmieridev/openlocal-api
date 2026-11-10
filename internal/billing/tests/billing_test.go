package billing_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/billing"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	"github.com/palmieridev/openlocal-api/internal/platform/logger"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestReconcilerRequiresCompleteBillingConfiguration(t *testing.T) {
	partial := billing.NewHandler(api.Runtime{}, config.Config{StripeSecretKey: "sk_test"})
	if partial.Reconciler() != nil {
		t.Fatal("partial Stripe configuration enabled background reconciliation")
	}

	complete := billing.NewHandler(api.Runtime{}, config.Config{
		StripeSecretKey:         "sk_test",
		StripeWebhookSecret:     "whsec_test",
		StripeEssentialPrice:    "price_e",
		StripeBusinessPrice:     "price_b",
		StripeSecondModulePrice: "price_a",
	})
	if complete.Reconciler() == nil {
		t.Fatal("complete Stripe configuration disabled background reconciliation")
	}
}

func TestReconcileUsesRetrievedStripeState(t *testing.T) {
	pool, q, _ := database(t)
	ctx := context.Background()
	customer := "cus_" + uuid.NewString()
	event := "evt_" + uuid.NewString()
	business, err := q.CreateBusiness(ctx, db.CreateBusinessParams{Name: "Billing test", Slug: "billing-" + uuid.NewString(), BusinessType: "servicios", Status: "active", Timezone: "America/Mexico_City", LocationMode: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM billing_events WHERE id=$1", event)
		_, _ = pool.Exec(ctx, "DELETE FROM businesses WHERE id=$1", business.ID)
	})
	if err = q.EnsureSubscription(ctx, business.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE business_subscriptions SET stripe_customer_id=$2 WHERE business_id=$1", business.ID, customer); err != nil {
		t.Fatal(err)
	}
	if _, err = q.SaveBillingEvent(ctx, db.SaveBillingEventParams{ID: event, CustomerID: customer}); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour).Unix()
	end := time.Now().AddDate(0, 1, 0).Unix()
	subscriptionID := "sub_" + uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"`+subscriptionID+`","customer":"`+customer+`","status":"active","metadata":{"base_module":"appointments"},"items":{"has_more":false,"data":[{"id":"si_test","quantity":1,"current_period_start":`+strconv.FormatInt(start, 10)+`,"current_period_end":`+strconv.FormatInt(end, 10)+`,"price":{"id":"price_e","currency":"mxn","active":true}}]}}],"has_more":false}`)
	}))
	defer server.Close()
	h := billing.NewHandler(api.NewRuntime(logger.New("test"), pool, nil), config.Config{StripeSecretKey: "sk_test", StripeEssentialPrice: "price_e", StripeBusinessPrice: "price_b", StripeSecondModulePrice: "price_a"})
	h.Stripe = &billing.Client{HTTP: server.Client(), BaseURL: server.URL, Secret: "sk_test"}
	if err = h.Reconcile(ctx, customer); err != nil {
		t.Fatal(err)
	}
	stored, err := q.GetSubscription(ctx, business.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Tier != "essential" || stored.Status != "active" || len(stored.Modules) != 1 || stored.Modules[0] != "appointments" {
		t.Fatalf("unexpected subscription: %+v", stored)
	}
	var status string
	if err = pool.QueryRow(ctx, "SELECT status FROM billing_events WHERE id=$1", event).Scan(&status); err != nil || status != "processed" {
		t.Fatalf("event=%q err=%v", status, err)
	}
}

func TestReconcileOnlyGrantsProviderAppliedModules(t *testing.T) {
	ctx := context.Background()
	pool, q, _ := database(t)
	for _, pending := range []bool{false, true} {
		t.Run(strconv.FormatBool(pending), func(t *testing.T) {
			customer := "cus_" + uuid.NewString()
			subscriptionID := "sub_" + uuid.NewString()
			business, err := q.CreateBusiness(ctx, db.CreateBusinessParams{Name: "Billing access test", Slug: "billing-access-" + uuid.NewString(), BusinessType: "servicios", Status: "active", Timezone: "America/Mexico_City", LocationMode: "fixed"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM businesses WHERE id=$1", business.ID) })
			if err = q.EnsureSubscription(ctx, business.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, "UPDATE business_subscriptions SET stripe_customer_id=$2 WHERE business_id=$1", business.ID, customer); err != nil {
				t.Fatal(err)
			}
			start, end := time.Now().Add(-time.Hour).Unix(), time.Now().AddDate(0, 1, 0).Unix()
			items := `{"id":"si_base","quantity":1,"current_period_start":` + strconv.FormatInt(start, 10) + `,"current_period_end":` + strconv.FormatInt(end, 10) + `,"price":{"id":"price_e","currency":"mxn","active":true}}`
			invoice, pendingUpdate := `{"status":"paid","amount_due":0,"amount_paid":0,"ending_balance":956}`, `null`
			if pending {
				invoice, pendingUpdate = `{"status":"open","amount_due":956}`, `{"subscription_items":[{"price":"price_a"}]}`
			} else {
				items += `,{"id":"si_addon","quantity":1,"price":{"id":"price_a","currency":"mxn","active":true}}`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"id":"`+subscriptionID+`","customer":"`+customer+`","status":"active","metadata":{"base_module":"appointments","modules":"appointments,preorders"},"pending_update":`+pendingUpdate+`,"latest_invoice":`+invoice+`,"items":{"data":[`+items+`]}}]}`)
			}))
			defer server.Close()
			h := billing.NewHandler(api.NewRuntime(logger.New("test"), pool, nil), config.Config{StripeEssentialPrice: "price_e", StripeBusinessPrice: "price_b", StripeSecondModulePrice: "price_a"})
			h.Stripe = &billing.Client{HTTP: server.Client(), BaseURL: server.URL, Secret: "sk_test"}
			if err = h.Reconcile(ctx, customer); err != nil {
				t.Fatal(err)
			}
			stored, err := q.GetSubscription(ctx, business.ID)
			if err != nil {
				t.Fatal(err)
			}
			access, err := entitlements.Get(ctx, q, stored, "owner", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if access.Modules["preorders"].Owned == pending {
				t.Fatalf("pending=%v incorrectly grants preorder access: %+v", pending, access.Modules["preorders"])
			}
		})
	}
}

func TestChangeInputNormalizesModules(t *testing.T) {
	in := application.ChangeInput{Tier: "business", Modules: []string{"preorders", "appointments"}}
	if err := in.Validate(); err != nil || strings.Join(in.Modules, ",") != "appointments,preorders" {
		t.Fatalf("%v %v", in.Modules, err)
	}
	in.Modules = []string{"appointments", "appointments"}
	if err := in.Validate(); err == nil {
		t.Fatal("duplicate accepted")
	}
}
