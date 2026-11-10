package billing_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/billing"
	"github.com/palmieridev/openlocal-api/internal/config"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestRegisteredBillingAuthorizationReplayAndProjection(t *testing.T) {
	pool, q, _ := database(t)
	id := business(t, pool, q)
	foreign := business(t, pool, q)
	org := "org_" + uuid.NewString()
	if _, err := pool.Exec(t.Context(), "UPDATE businesses SET clerk_org_id=$2 WHERE id=$1", id, org); err != nil {
		t.Fatal(err)
	}
	users := map[string]string{}
	for _, role := range []string{"owner", "manager", "staff"} {
		clerkID := "user_" + uuid.NewString()
		users[role] = clerkID
		u, err := q.UpsertUserFromClerk(t.Context(), db.UpsertUserFromClerkParams{ClerkUserID: clerkID})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", u.ID)
			if err != nil {
				t.Error(err)
			}
		})
		if _, err = q.AddBusinessMember(t.Context(), db.AddBusinessMemberParams{BusinessID: id, UserID: u.ID, ClerkOrgID: org, Role: role}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(t.Context(), "UPDATE business_subscriptions SET stripe_customer_id=$2 WHERE business_id=$1", id, "cus_"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/v1/billing_portal/sessions" {
			t.Errorf("unexpected provider call: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("return_url") != "https://openlocal.store/en/dashboard/billing" || r.Form.Get("configuration") != "bpc_local" || r.Header.Get("Idempotency-Key") != id.String()+":portal:portal-key" {
			t.Errorf("incorrect portal form/key: %v %s", r.Form, r.Header.Get("Idempotency-Key"))
		}
		_, _ = io.WriteString(w, `{"url":"https://billing.stripe.com/local"}`)
	}))
	defer provider.Close()
	cfg := config.Config{AppBaseURL: "https://openlocal.store", StripeSecretKey: "sk_local", StripeWebhookSecret: "whsec_local", StripeEssentialPrice: "price_e", StripeBusinessPrice: "price_b", StripeSecondModulePrice: "price_a", StripePortalConfiguration: "bpc_local"}
	h := billing.NewHandler(api.NewRuntime(nil, pool, nil), cfg)
	h.Stripe = &billing.Client{HTTP: provider.Client(), BaseURL: provider.URL, Secret: "sk_local"}
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	h.RegisterPrivateRoutes(app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
	request := func(method, path, role, body, key string) (int, map[string]json.RawMessage) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/businesses/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		if role != "" {
			req.Header.Set("X-Test-Clerk-User-ID", users[role])
			req.Header.Set("X-Test-Clerk-Org-ID", org)
			// A claimed Clerk admin role cannot bypass the local membership role.
			req.Header.Set("X-Test-Clerk-Org-Role", "org:admin")
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result map[string]json.RawMessage
		if err = json.NewDecoder(res.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode == 200 && !strings.Contains(path, "/billing/") && res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("private billing read is cacheable")
		}
		return res.StatusCode, result
	}
	for _, role := range []string{"", "owner", "manager", "staff"} {
		status, _ := request("GET", id.String()+"/subscription", role, "", "")
		want := 403
		if role == "" {
			want = 401
		}
		if role == "owner" {
			want = 200
		}
		if status != want {
			t.Fatalf("subscription role=%q status=%d want=%d", role, status, want)
		}
		if role != "owner" {
			status, _ = request("POST", id.String()+"/billing/portal", role, "invalid JSON", "invalid")
			if status != want {
				t.Fatalf("portal role=%q status=%d want=%d", role, status, want)
			}
		}
		status, _ = request("GET", id.String()+"/entitlements", role, "", "")
		want = 200
		if role == "" {
			want = 401
		}
		if status != want {
			t.Fatalf("entitlements role=%q status=%d want=%d", role, status, want)
		}
	}
	for _, target := range []uuid.UUID{foreign, uuid.New()} {
		status, _ := request("GET", target.String()+"/subscription", "owner", "", "")
		if status != 403 {
			t.Fatalf("foreign/missing business revealed: %d", status)
		}
	}
	status, result := request("GET", id.String()+"/subscription", "owner", "", "")
	for _, key := range []string{"tier", "status", "modules", "period_start", "period_end", "grace_until", "cancel_at_period_end", "scheduled_change", "billing_available", "entitlements"} {
		if _, ok := result[key]; !ok {
			t.Fatalf("missing subscription field %q: %v", key, result)
		}
	}
	if status != 200 || len(result) != 10 {
		t.Fatalf("subscription projection changed: %d %+v", status, result)
	}
	for _, body := range []string{`{"locale":"en","unknown":true}`, `{"locale":"https://evil.example"}`} {
		status, _ = request("POST", id.String()+"/billing/portal", "owner", body, "portal-key")
		if status != 400 {
			t.Fatalf("invalid portal body accepted: %d", status)
		}
	}
	status, result = request("POST", id.String()+"/billing/portal", "owner", `{}`, "short")
	if status != 400 || result["code"] != nil {
		t.Fatalf("optional code behavior changed for key validation: %d %v", status, result)
	}
	for i := 0; i < 2; i++ {
		status, result = request("POST", id.String()+"/billing/portal", "owner", `{"locale":"en"}`, "portal-key")
		if status != 200 || string(result["url"]) != `"https://billing.stripe.com/local"` {
			t.Fatalf("portal result: %d %s", status, result["url"])
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("rejected requests/replay reached provider: %d calls", calls.Load())
	}
}
