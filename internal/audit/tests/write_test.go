package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/palmieridev/openlocal-api/internal/audit"
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

func TestAuditWithMetadata(t *testing.T) {
	businessID, userID, entityID := uuid.New(), uuid.New(), uuid.New()
	recorder := &queryRecorder{}
	q := db.New(recorder)
	metadata := map[string]any{"before": map[string]any{"tier": "free"}, "after": map[string]any{"tier": "essential"}, "reason": "pilot"}
	if err := audit.WithMetadata(q, context.Background(), businessID, userID, "platform_admin.grant_created", "access_grant", entityID, metadata); err != nil {
		t.Fatal(err)
	}
	if recorder.calls != 1 || recorder.args[0].(uuid.NullUUID).UUID != businessID || recorder.args[1].(uuid.NullUUID).UUID != userID || recorder.args[4].(uuid.NullUUID).UUID != entityID {
		t.Fatalf("unexpected audit arguments: %v", recorder.args)
	}
	var stored map[string]any
	if err := json.Unmarshal(recorder.args[5].([]byte), &stored); err != nil || stored["reason"] != "pilot" {
		t.Fatalf("metadata=%v err=%v", stored, err)
	}
	if err := audit.WithMetadata(q, context.Background(), uuid.Nil, uuid.Nil, "platform_admin.test", "test", uuid.Nil, nil); err != nil {
		t.Fatal(err)
	}
	if recorder.args[0].(uuid.NullUUID).Valid || recorder.args[1].(uuid.NullUUID).Valid || recorder.args[4].(uuid.NullUUID).Valid || string(recorder.args[5].([]byte)) != "{}" {
		t.Fatal("nil values not represented as documented")
	}
	calls := recorder.calls
	if err := audit.WithMetadata(q, context.Background(), businessID, userID, "test", "test", entityID, map[string]any{"bad": make(chan int)}); err == nil || recorder.calls != calls {
		t.Fatal("invalid metadata must fail before insertion")
	}
	want := errors.New("insert failed")
	recorder.err = want
	if err := audit.WithMetadata(q, context.Background(), businessID, userID, "test", "test", entityID, nil); !errors.Is(err, want) {
		t.Fatalf("got %v want insert error", err)
	}
}

func TestAuditPreservesExistingBehavior(t *testing.T) {
	recorder := &queryRecorder{}
	if err := audit.Write(db.New(recorder), context.Background(), uuid.Nil, uuid.Nil, "test", "test", uuid.Nil); err != nil {
		t.Fatal(err)
	}
	if !recorder.args[0].(uuid.NullUUID).Valid || !recorder.args[1].(uuid.NullUUID).Valid || !recorder.args[4].(uuid.NullUUID).Valid || string(recorder.args[5].([]byte)) != "{}" {
		t.Fatal("legacy audit.Write behavior changed")
	}
}
