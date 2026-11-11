package platformadmin_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func adminPool(t *testing.T) *pgxpool.Pool {
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
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = pool.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	return pool
}
func createUser(t *testing.T, ctx context.Context, q *db.Queries) db.User {
	t.Helper()
	u, err := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: "user_admin_test_" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func createBusiness(t *testing.T, ctx context.Context, q *db.Queries) db.Business {
	t.Helper()
	b, err := q.CreateBusiness(ctx, db.CreateBusinessParams{Name: "Admin test", Slug: "admin-test-" + uuid.NewString(), BusinessType: "retail", Status: "active", Timezone: "UTC", LocationMode: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
