package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/platform/logger"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestVerifySignature(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	body := []byte(`{"id":"evt_test"}`)
	timestamp := "2000000000"
	mac := hmac.New(sha256.New, []byte("whsec_test"))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	header := "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	if !verifySignature(header, "whsec_test", body, now) {
		t.Fatal("valid signature rejected")
	}
	if verifySignature(header, "wrong", body, now) || verifySignature(header, "whsec_test", append(body, 'x'), now) || verifySignature(header, "whsec_test", body, now.Add(6*time.Minute)) {
		t.Fatal("invalid signature accepted")
	}
}

func TestStripeClientPinsVersionAndIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Stripe-Version") != StripeVersion {
			t.Error("missing version")
		}
		if r.Header.Get("Idempotency-Key") != "request-key" {
			t.Error("missing key")
		}
		user, _, ok := r.BasicAuth()
		if !ok || user != "sk_test" {
			t.Error("missing auth")
		}
		data, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(data))
		if values.Get("currency") != "mxn" {
			t.Error("missing form")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), BaseURL: server.URL, Secret: "sk_test"}
	var out struct {
		ID string `json:"id"`
	}
	if err := client.call(t.Context(), http.MethodPost, "/test", "request-key", url.Values{"currency": {"mxn"}}, &out); err != nil || out.ID != "ok" {
		t.Fatalf("%v %#v", err, out)
	}
}

func TestStripeClientMapsFailuresWithoutLeakingProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`secret provider detail`))
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), BaseURL: server.URL, Secret: "sk_test"}
	err := client.call(t.Context(), http.MethodGet, "/test", "", nil, &struct{}{})
	domain, ok := err.(*api.DomainError)
	if !ok || domain.Code != "billing_unavailable" || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestInterpretSubscriptionRequiresKnownMonthlyComposition(t *testing.T) {
	h := NewHandler(api.Runtime{}, config.Config{StripeEssentialPrice: "price_e", StripeBusinessPrice: "price_b", StripeSecondModulePrice: "price_a"})
	base := item{Quantity: 1, Price: price{ID: "price_e", Currency: "mxn", Active: true}}
	sub := subscription{Metadata: map[string]string{"base_module": "appointments"}}
	sub.Items.Data = []item{base}
	tier, modules, err := h.interpret(sub)
	if err != nil || tier != "essential" || len(modules) != 1 || modules[0] != "appointments" {
		t.Fatalf("%s %v %v", tier, modules, err)
	}
	addon := base
	addon.Price.ID = "price_a"
	sub.Items.Data = append(sub.Items.Data, addon)
	_, modules, err = h.interpret(sub)
	if err != nil || len(modules) != 2 {
		t.Fatalf("addon: %v %v", modules, err)
	}
	sub.Items.Data[1].Price.ID = "unknown"
	if _, _, err = h.interpret(sub); err == nil {
		t.Fatal("unknown price accepted")
	}
}

func TestChangeInputNormalizesModules(t *testing.T) {
	in := changeInput{Tier: "business", Modules: []string{"preorders", "appointments"}}
	if err := in.validate(); err != nil || strings.Join(in.Modules, ",") != "appointments,preorders" {
		t.Fatalf("%v %v", in.Modules, err)
	}
	in.Modules = []string{"appointments", "appointments"}
	if err := in.validate(); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestReconcilerRequiresCompleteBillingConfiguration(t *testing.T) {
	partial := NewHandler(api.Runtime{}, config.Config{StripeSecretKey: "sk_test"})
	if partial.Reconciler() != nil {
		t.Fatal("partial Stripe configuration enabled background reconciliation")
	}

	complete := NewHandler(api.Runtime{}, config.Config{
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
	if os.Getenv("OPENLOCAL_INTEGRATION_TESTS") != "1" {
		t.Skip("requires isolated Docker PostgreSQL")
	}
	dsn := os.Getenv("DATABASE_URL")
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := db.New(pool)
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
	sub, err := q.GetSubscription(ctx, business.ID)
	if err != nil {
		t.Fatal(err)
	}
	params := updateParams(sub)
	params.StripeCustomerID = sqlText(customer)
	if _, err = q.UpdateSubscription(ctx, params); err != nil {
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
	h := NewHandler(api.NewRuntime(logger.New("test"), pool, nil), config.Config{StripeSecretKey: "sk_test", StripeEssentialPrice: "price_e", StripeBusinessPrice: "price_b", StripeSecondModulePrice: "price_a"})
	h.Stripe = &Client{HTTP: server.Client(), BaseURL: server.URL, Secret: "sk_test"}
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
