package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
			rt := Runtime{Q: db.New(recorder)}
			app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
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

func TestAuditWithMetadata(t *testing.T) {
	businessID, userID, entityID := uuid.New(), uuid.New(), uuid.New()
	recorder := &queryRecorder{}
	q := db.New(recorder)
	metadata := map[string]any{"before": map[string]any{"tier": "free"}, "after": map[string]any{"tier": "essential"}, "reason": "pilot"}
	if err := AuditWithMetadata(q, context.Background(), businessID, userID, "platform_admin.grant_created", "access_grant", entityID, metadata); err != nil {
		t.Fatal(err)
	}
	if recorder.calls != 1 || recorder.args[0].(uuid.NullUUID).UUID != businessID || recorder.args[1].(uuid.NullUUID).UUID != userID || recorder.args[4].(uuid.NullUUID).UUID != entityID {
		t.Fatalf("unexpected audit arguments: %v", recorder.args)
	}
	var stored map[string]any
	if err := json.Unmarshal(recorder.args[5].([]byte), &stored); err != nil || stored["reason"] != "pilot" {
		t.Fatalf("metadata=%v err=%v", stored, err)
	}
	if err := AuditWithMetadata(q, context.Background(), uuid.Nil, uuid.Nil, "platform_admin.test", "test", uuid.Nil, nil); err != nil {
		t.Fatal(err)
	}
	if recorder.args[0].(uuid.NullUUID).Valid || recorder.args[1].(uuid.NullUUID).Valid || recorder.args[4].(uuid.NullUUID).Valid || string(recorder.args[5].([]byte)) != "{}" {
		t.Fatal("nil values not represented as documented")
	}
	calls := recorder.calls
	if err := AuditWithMetadata(q, context.Background(), businessID, userID, "test", "test", entityID, map[string]any{"bad": make(chan int)}); err == nil || recorder.calls != calls {
		t.Fatal("invalid metadata must fail before insertion")
	}
	want := errors.New("insert failed")
	recorder.err = want
	if err := AuditWithMetadata(q, context.Background(), businessID, userID, "test", "test", entityID, nil); !errors.Is(err, want) {
		t.Fatalf("got %v want insert error", err)
	}
}

func TestAuditPreservesExistingBehavior(t *testing.T) {
	recorder := &queryRecorder{}
	if err := Audit(db.New(recorder), context.Background(), uuid.Nil, uuid.Nil, "test", "test", uuid.Nil); err != nil {
		t.Fatal(err)
	}
	if !recorder.args[0].(uuid.NullUUID).Valid || !recorder.args[1].(uuid.NullUUID).Valid || !recorder.args[4].(uuid.NullUUID).Valid || string(recorder.args[5].([]byte)) != "{}" {
		t.Fatal("legacy Audit behavior changed")
	}
}
