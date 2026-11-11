package accessgrants_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/accessgrants"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/application"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/grantpostgres"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/platformadmin/adminpostgres"
	admin "github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

type administrationFixture struct {
	pool            *pgxpool.Pool
	q               *db.Queries
	business, other db.Business
	actor, ordinary db.User
	service         *application.Service
	app             *fiber.App
}

func administrationSetup(t *testing.T) administrationFixture {
	t.Helper()
	dsn := os.Getenv("OPENLOCAL_BILLING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicit migrated disposable OPENLOCAL_BILLING_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.ConnConfig.Database, "openlocal_billing_test") || cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" {
		t.Fatal("requires disposable local openlocal_billing_test* database")
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "grant-administration-" + uuid.NewString()
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := administrationFixture{pool: pool, q: db.New(pool)}
	makeUser := func() db.User {
		u, err := f.q.UpsertUserFromClerk(t.Context(), db.UpsertUserFromClerkParams{ClerkUserID: "user_grant_admin_" + uuid.NewString()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", u.ID)
			if err != nil {
				t.Error(err)
			}
		})
		return u
	}
	makeBusiness := func() db.Business {
		b, err := f.q.CreateBusiness(t.Context(), db.CreateBusinessParams{Name: "Grant administration", Slug: "grant-admin-" + uuid.NewString(), BusinessType: "services", Status: "active", Timezone: "UTC", LocationMode: "fixed"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, err := pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE business_id=$1", b.ID)
			if err != nil {
				t.Error(err)
			}
			_, err = pool.Exec(context.Background(), "DELETE FROM businesses WHERE id=$1", b.ID)
			if err != nil {
				t.Error(err)
			}
		})
		return b
	}
	f.actor = makeUser()
	f.ordinary = makeUser()
	f.business = makeBusiness()
	f.other = makeBusiness()
	store := grantpostgres.New(pool, f.q)
	f.service = application.New(admin.NewAuthorizer(adminpostgres.New(f.q), []string{f.actor.ClerkUserID}), store, store)
	f.app = fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	accessgrants.NewHandler(api.NewRuntime(nil, pool, nil), []string{f.actor.ClerkUserID}).RegisterRoutes(f.app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
	return f
}
func (f administrationFixture) identity() admin.Actor {
	return admin.Actor{ClerkUserID: f.actor.ClerkUserID, UserID: f.actor.ID}
}
func (f administrationFixture) key(grant uuid.UUID) application.ScopedID {
	return application.ScopedID{BusinessID: f.business.ID, ID: grant}
}
func (f administrationFixture) create(t *testing.T) application.Grant {
	t.Helper()
	end := time.Now().Add(time.Hour)
	g, err := f.service.Create(t.Context(), f.identity(), f.business.ID, application.CreateInput{Module: "appointments", Tier: "business", StartsAt: time.Now().Add(-time.Minute), ExpiresAt: &end, InternalReason: "private gift", OwnerMessage: stringPointer("Enjoy")})
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func stringPointer(value string) *string { return &value }
func (f administrationFixture) request(t *testing.T, user, method, path string, body any) (int, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, "/api/v1/admin/businesses/"+f.business.ID.String()+"/grants"+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Clerk-User-ID", user)
	req.Header.Set("X-Test-Clerk-Org-ID", "org_forged")
	req.Header.Set("X-Test-Clerk-Org-Role", "org:admin")
	res, err := f.app.Test(req, 10000)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err = io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("admin response may be cached")
	}
	return res.StatusCode, data
}
func (f administrationFixture) counts(t *testing.T, business uuid.UUID) (int, int, int) {
	t.Helper()
	var grants, audits, subs int
	if err := f.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM business_access_grants WHERE business_id=$1),(SELECT count(*) FROM audit_logs WHERE business_id=$1),(SELECT count(*) FROM business_subscriptions WHERE business_id=$1)", business).Scan(&grants, &audits, &subs); err != nil {
		t.Fatal(err)
	}
	return grants, audits, subs
}
func (f administrationFixture) hold(t *testing.T) (func(), func(int)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()); cancel() })
	if _, err = f.q.WithTx(tx).LockSubscription(ctx, f.business.ID); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err = tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return func() {
			t.Helper()
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}, func(n int) {
			t.Helper()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				var count int
				err := f.pool.QueryRow(ctx, `WITH RECURSIVE blocked(pid) AS (
    SELECT $1::integer UNION
    SELECT a.pid FROM pg_stat_activity a JOIN blocked b ON b.pid=ANY(pg_blocking_pids(a.pid))
    WHERE a.datname=current_database() AND a.wait_event_type='Lock'
   ) SELECT count(*)-1 FROM blocked`, pid).Scan(&count)
				if err != nil {
					t.Fatal(err)
				}
				if count >= n {
					return
				}
				select {
				case <-ctx.Done():
					t.Fatalf("only %d/%d operations reached subscription lock", count, n)
				case <-ticker.C:
				}
			}
		}
}
