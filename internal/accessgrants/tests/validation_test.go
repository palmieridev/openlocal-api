package accessgrants_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/accessgrants"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestCreateInputValidation(t *testing.T) {
	dsn := os.Getenv("OPENLOCAL_BILLING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicit migrated disposable OPENLOCAL_BILLING_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.ConnConfig.Database, "openlocal_billing_test") || cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" {
		t.Fatal("grant validation requires a disposable local openlocal_billing_test* database")
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := db.New(pool)
	clerkID := "user_grant_validation_" + uuid.NewString()
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
	b, err := q.CreateBusiness(t.Context(), db.CreateBusinessParams{Name: "Grant validation", Slug: "grant-validation-" + uuid.NewString(), BusinessType: "servicios", Status: "active", Timezone: "UTC", LocationMode: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM businesses WHERE id=$1", b.ID)
		if err != nil {
			t.Error(err)
		}
	})
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	accessgrants.NewHandler(api.NewRuntime(nil, pool, nil), []string{clerkID}).RegisterRoutes(app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
	request := func(in accessgrants.CreateInput, want int) {
		t.Helper()
		body, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/api/v1/admin/businesses/"+b.ID.String()+"/grants", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Clerk-User-ID", clerkID)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("status=%d want=%d input=%s", res.StatusCode, want, body)
		}
		if want == 400 {
			var result api.ErrorResponse
			if err = json.NewDecoder(res.Body).Decode(&result); err != nil || result.Code != "invalid_request" {
				t.Fatalf("invalid input contract: %+v %v", result, err)
			}
		}
	}
	now := time.Now()
	valid := accessgrants.CreateInput{Module: "appointments", Tier: "essential", StartsAt: now, Indefinite: true, InternalReason: "gift"}
	request(valid, 201)
	cases := []struct {
		name  string
		alter func(*accessgrants.CreateInput)
	}{
		{"module", func(v *accessgrants.CreateInput) { v.Module = "pickup" }},
		{"tier", func(v *accessgrants.CreateInput) { v.Tier = "free" }},
		{"start", func(v *accessgrants.CreateInput) { v.StartsAt = time.Time{} }},
		{"reason", func(v *accessgrants.CreateInput) { v.InternalReason = " " }},
		{"expiry choice", func(v *accessgrants.CreateInput) { v.Indefinite = false }},
		{"conflicting expiry", func(v *accessgrants.CreateInput) { v.ExpiresAt = &now }},
		{"empty owner message", func(v *accessgrants.CreateInput) { v.OwnerMessage = new(string) }},
		{"expired", func(v *accessgrants.CreateInput) {
			past := now.Add(-time.Second)
			v.StartsAt = now.Add(-time.Hour)
			v.Indefinite = false
			v.ExpiresAt = &past
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { in := valid; tc.alter(&in); request(in, 400) })
	}
	count, err := q.CountAccessGrants(t.Context(), b.ID)
	if err != nil || count != 1 {
		t.Fatalf("invalid input persisted grants: count=%d err=%v", count, err)
	}
}
