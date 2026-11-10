package billing_test

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/billing"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/config"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func payment(server *httptest.Server, cfg config.Config) application.Payment {
	return billing.NewPayment(cfg, &billing.Client{HTTP: server.Client(), BaseURL: server.URL, Secret: "sk_test"})
}
func database(t *testing.T) (*pgxpool.Pool, *db.Queries, string) {
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
		t.Fatal("billing tests require a disposable local database named openlocal_billing_test*")
	}
	name := "billing-test-" + uuid.NewString()
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, db.New(pool), name
}
func business(t *testing.T, pool *pgxpool.Pool, q *db.Queries) uuid.UUID {
	t.Helper()
	b, err := q.CreateBusiness(t.Context(), db.CreateBusinessParams{Name: "Billing core", Slug: "billing-core-" + uuid.NewString(), BusinessType: "servicios", Status: "active", Timezone: "UTC", LocationMode: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM businesses WHERE id=$1", b.ID)
		if err != nil {
			t.Error(err)
		}
	})
	if err = q.EnsureSubscription(t.Context(), b.ID); err != nil {
		t.Fatal(err)
	}
	return b.ID
}
