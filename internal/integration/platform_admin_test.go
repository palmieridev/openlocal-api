package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/palmieridev/openlocal-api/internal/audit"
	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestAuditMetadataUsesCallerTransaction(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	outer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Rollback(ctx)
	q := db.New(outer)
	actor := createUser(t, ctx, q)
	business := createBusiness(t, ctx, q)
	entity := uuid.New()
	nested, err := outer.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.WithMetadata(db.New(nested), ctx, business.ID, actor.ID, "platform_admin.test", "business", entity, map[string]any{"before": map[string]any{"tier": "free"}, "after": map[string]any{"tier": "essential"}, "reason": "private pilot"}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.New(nested).AdminListActivity(ctx, db.AdminListActivityParams{BusinessID: uuid.NullUUID{UUID: business.ID, Valid: true}, LimitCount: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(rows[0].Metadata, &metadata); err != nil || metadata["reason"] != "private pilot" {
		t.Fatalf("metadata=%v err=%v", metadata, err)
	}
	if rows[0].ActorUserID.UUID != actor.ID || rows[0].EntityID.UUID != entity {
		t.Fatal("audit attribution changed")
	}
	if err := nested.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err = q.AdminListActivity(ctx, db.AdminListActivityParams{BusinessID: uuid.NullUUID{UUID: business.ID, Valid: true}, LimitCount: 10})
	if err != nil || len(rows) != 0 {
		t.Fatalf("audit escaped transaction: rows=%v err=%v", rows, err)
	}
}
