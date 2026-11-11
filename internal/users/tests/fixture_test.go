package users_test

import (
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
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/users"
)

type fixture struct {
	pool     *pgxpool.Pool
	q        *db.Queries
	app      *fiber.App
	rt       api.Runtime
	identity auth.Context
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("OPENLOCAL_CATALOG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicit migrated disposable OPENLOCAL_CATALOG_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.ConnConfig.Database, "openlocal_catalog_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("requires local disposable openlocal_catalog_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	f := &fixture{pool: pool, q: db.New(pool), rt: api.NewRuntime(nil, pool, nil), app: fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})}
	f.identity = auth.Context{ClerkUserID: "user_users_" + uuid.NewString(), ClerkOrgID: "org_users_" + uuid.NewString(), Role: "owner"}
	user, err := f.q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: f.identity.ClerkUserID})
	if err != nil {
		t.Fatal(err)
	}
	f.identity.UserID = user.ID
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", user.ID)
		if err != nil {
			t.Error(err)
		}
	})
	users.NewHandler(f.rt).RegisterRoutes(f.app.Group("", func(c *fiber.Ctx) error { auth.SetFiber(c, f.identity); return c.Next() }))
	return f
}
func (f *fixture) request(t *testing.T, method, path, body string, status int) []byte {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := f.app.Test(req, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: status %d want %d: %s", method, path, res.StatusCode, status, data)
	}
	return data
}
func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
