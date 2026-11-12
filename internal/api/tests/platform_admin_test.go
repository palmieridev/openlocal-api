package api_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

// queryRecorder fails on unexpected database access, including denied requests.
type queryRecorder struct {
	args  []any
	calls int
	err   error
}

func (r *queryRecorder) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}
func (r *queryRecorder) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}
func (r *queryRecorder) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	r.calls++
	r.args = args
	return recordedRow{r.err}
}

type recordedRow struct{ err error }

func (r recordedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	// UpsertUserFromClerk and CreateAuditLog both return their primary ID first.
	*dest[0].(*uuid.UUID) = uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	return nil
}

func TestRequirePlatformAdmin(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity *auth.Context
		allowed  []string
		status   int
		calls    int
	}{
		{name: "missing context", allowed: []string{"user_admin"}, status: 401},
		{name: "empty user", identity: &auth.Context{}, allowed: []string{""}, status: 401},
		{name: "missing allowlist", identity: &auth.Context{ClerkUserID: "user_admin"}, status: 403},
		{name: "org admin denied", identity: &auth.Context{ClerkUserID: "user_owner", ClerkOrgID: "org_1", OrgRole: "org:admin", Role: "owner"}, allowed: []string{"user_admin"}, status: 403},
		{name: "exact match required", identity: &auth.Context{ClerkUserID: "user_admin_suffix"}, allowed: []string{"user_admin"}, status: 403},
		{name: "case sensitive", identity: &auth.Context{ClerkUserID: "USER_admin"}, allowed: []string{"user_admin"}, status: 403},
		{name: "allowed without org", identity: &auth.Context{ClerkUserID: "user_admin"}, allowed: []string{"user_admin"}, status: 200, calls: 1},
		{name: "allowed in unrelated org", identity: &auth.Context{ClerkUserID: "user_admin", ClerkOrgID: "org_other", Role: "staff"}, allowed: []string{"user_admin"}, status: 200, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &queryRecorder{}
			rt := api.Runtime{Q: db.New(recorder)}
			app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
			app.Get("/", func(c *fiber.Ctx) error {
				if tc.identity != nil {
					auth.SetFiber(c, *tc.identity)
				}
				a, u, err := rt.RequirePlatformAdmin(c, tc.allowed)
				if err != nil {
					return err
				}
				if u.ID == uuid.Nil || a.UserID != u.ID {
					t.Error("local actor was not resolved")
				}
				return c.SendStatus(200)
			})
			res, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status || recorder.calls != tc.calls {
				t.Fatalf("status/calls=%d/%d want %d/%d", res.StatusCode, recorder.calls, tc.status, tc.calls)
			}
		})
	}
}
