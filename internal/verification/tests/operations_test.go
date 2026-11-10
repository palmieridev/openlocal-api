package verification_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOperationsProviderHTTPFailureFailsClosed(t *testing.T) {
	dsn := os.Getenv("OPENLOCAL_IDENTITY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires migrated disposable OPENLOCAL_IDENTITY_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") || !strings.HasPrefix(cfg.ConnConfig.Database, "openlocal_identity_test") {
		t.Fatal("requires disposable local database")
	}
	cfg.ConnConfig.ConnectTimeout = 3 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	ctx := context.Background()
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	b, err := db.New(p).CreateBusiness(ctx, db.CreateBusinessParams{ClerkOrgID: sql.NullString{String: "org_" + uuid.NewString(), Valid: true}, Name: "Challenge fixture", Slug: "challenge-" + uuid.NewString(), Status: "active", BusinessType: "retail", Timezone: "America/Mexico_City", LocationMode: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, err := p.Exec(ctx, "DELETE FROM businesses WHERE id=$1", b.ID)
		if err != nil {
			t.Error(err)
		}
	}()
	if err := db.New(p).EnsureSubscription(ctx, b.ID); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, token, reply         string
		status, want, wantCalls    int
		unavailable, missingSecret bool
	}{
		{name: "provider HTTP failure", token: "token", reply: `{"success":true,"hostname":"shop.example","action":"request"}`, status: 503, want: 503, wantCalls: 1},
		{name: "wrong hostname", token: "token", reply: `{"success":true,"hostname":"evil.example","action":"request"}`, status: 200, want: 400, wantCalls: 1},
		{name: "wrong action", token: "token", reply: `{"success":true,"hostname":"shop.example","action":"login"}`, status: 200, want: 400, wantCalls: 1},
		{name: "rejected", token: "token", reply: `{"success":false}`, status: 200, want: 400, wantCalls: 1},
		{name: "malformed", token: "token", reply: `{`, status: 200, want: 400, wantCalls: 1},
		{name: "network failure", token: "token", unavailable: true, want: 503, wantCalls: 1},
		{name: "missing token", want: 400},
		{name: "oversized token", token: strings.Repeat("x", 2049), want: 400},
		{name: "missing secret", token: "token", missingSecret: true, want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := operations.NewHandler(api.NewRuntime(nil, p, nil), config.Config{AppEnv: "production", TurnstileSecret: "local-secret", TurnstileHostnames: []string{"shop.example"}, ModulesAllowTestChallenge: true})
			if tc.missingSecret {
				h.Config.TurnstileSecret = ""
			}
			h.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.unavailable {
					return nil, errors.New("fixture provider unavailable")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.reply)), Header: make(http.Header)}, nil
			})}
			app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
			h.RegisterPublicRoutes(app.Group("/api/v1"))
			input, err := json.Marshal(map[string]string{"customer_name": "Fixture Customer", "customer_phone": "+525555550001", "challenge_token": tc.token})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api/v1/public/businesses/"+b.Slug+"/appointments", bytes.NewReader(input))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", uuid.NewString())
			res, err := app.Test(req, 5000)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var body api.ErrorResponse
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("provider calls=%d want=%d (production bypass is forbidden)", calls, tc.wantCalls)
			}
			if res.StatusCode != tc.want || body.Code != "challenge_failed" {
				t.Fatalf("challenge response status=%d body=%+v", res.StatusCode, body)
			}
			var n int
			if err := p.QueryRow(ctx, "SELECT (SELECT count(*) FROM module_customers WHERE business_id=$1) + (SELECT count(*) FROM appointments WHERE business_id=$1)", b.ID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatal("failed challenge wrote customer or appointment")
			}
		})
	}
}
