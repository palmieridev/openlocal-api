package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/config"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/platformadmin"
)

func adminTestApp(q *db.Queries, allowed string) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	private := app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth())
	platformadmin.NewHandler(api.Runtime{Q: q}, config.Config{PlatformAdminUserIDs: []string{allowed}}).RegisterPrivateRoutes(private)
	return app
}
func adminGet[T any](t *testing.T, app *fiber.App, path, user string, want int) T {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("X-Test-Clerk-User-ID", user)
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		t.Fatalf("%s status=%d want %d", path, res.StatusCode, want)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("admin response may be cached")
	}
	var out T
	if want == 200 {
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestPlatformAdminBusinessSearchDetailAndActivity(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)
	actor := createUser(t, ctx, q)
	app := adminTestApp(q, actor.ClerkUserID)
	suffix := uuid.NewString()
	first := createBusiness(t, ctx, q)
	second := createBusiness(t, ctx, q)
	orgID := "org_admin_test_" + suffix
	// Deliberately tied timestamps exercise the UUID pagination tiebreaker.
	if _, err := tx.Exec(ctx, "UPDATE businesses SET name=$1,status='archived',created_at=$2 WHERE id IN ($3,$4)", "Admin %_ "+suffix, time.Now().UTC(), first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE businesses SET clerk_org_id=$1 WHERE id=$2", orgID, first.ID); err != nil {
		t.Fatal(err)
	}
	page := adminGet[api.CursorPage[platformadmin.BusinessResponse]](t, app, "/api/v1/admin/businesses?q="+url.QueryEscape(suffix)+"&limit=1", actor.ClerkUserID, 200)
	if len(page.Items) != 1 || page.NextCursor == nil || page.Limit != 1 {
		t.Fatalf("page=%+v", page)
	}
	next := adminGet[api.CursorPage[platformadmin.BusinessResponse]](t, app, "/api/v1/admin/businesses?q="+url.QueryEscape(suffix)+"&limit=1&cursor="+url.QueryEscape(*page.NextCursor), actor.ClerkUserID, 200)
	if len(next.Items) != 1 || next.NextCursor != nil || next.Items[0].ID == page.Items[0].ID {
		t.Fatalf("second page=%+v", next)
	}
	for _, query := range []string{first.ID.String(), first.Slug, orgID, "ADMIN %_ " + suffix} {
		found := adminGet[api.CursorPage[platformadmin.BusinessResponse]](t, app, "/api/v1/admin/businesses?q="+url.QueryEscape(query), actor.ClerkUserID, 200)
		if len(found.Items) == 0 {
			t.Fatalf("query %q did not match", query)
		}
		if query == orgID && (len(found.Items) != 1 || found.Items[0].ID != first.ID || found.Items[0].ClerkOrgID == nil || *found.Items[0].ClerkOrgID != orgID) {
			t.Fatalf("organization search=%+v", found)
		}
	}
	wildcard := adminGet[api.CursorPage[platformadmin.BusinessResponse]](t, app, "/api/v1/admin/businesses?q="+url.QueryEscape("%"+suffix), actor.ClerkUserID, 200)
	if len(wildcard.Items) != 0 {
		t.Fatal("search interpreted percent as SQL wildcard")
	}
	detail := adminGet[platformadmin.BusinessDetailResponse](t, app, "/api/v1/admin/businesses/"+first.ID.String(), actor.ClerkUserID, 200)
	if detail.Subscription != nil || detail.Modules["appointments"].Owned || detail.Modules["appointments"].Usage != 0 || detail.AccessRole != "owner" {
		t.Fatalf("missing subscription detail=%+v", detail)
	}
	if _, err := q.GetSubscription(ctx, first.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("inspection created subscription: %v", err)
	}
	if err := q.EnsureSubscription(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	start := pgtype.Timestamptz{Time: time.Now().UTC().Add(-time.Hour), Valid: true}
	end := pgtype.Timestamptz{Time: time.Now().UTC().Add(time.Hour), Valid: true}
	if _, err := q.UpdateSubscription(ctx, db.UpdateSubscriptionParams{BusinessID: first.ID, Tier: "essential", Status: "active", Modules: []string{"appointments"}, PeriodStart: start, PeriodEnd: end, StripeCustomerID: sql.NullString{String: "cus_private", Valid: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.UpsertModule(ctx, db.UpsertModuleParams{BusinessID: first.ID, Module: "appointments", Enabled: true, Settings: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := q.ConsumeModuleUsage(ctx, db.ConsumeModuleUsageParams{BusinessID: first.ID, Module: "appointments", RecordID: uuid.New(), PeriodStart: start}); err != nil {
		t.Fatal(err)
	}
	// Usage from a different billing period must not count.
	if err := q.ConsumeModuleUsage(ctx, db.ConsumeModuleUsageParams{BusinessID: first.ID, Module: "appointments", RecordID: uuid.New(), PeriodStart: pgtype.Timestamptz{Time: start.Time.Add(-24 * time.Hour), Valid: true}}); err != nil {
		t.Fatal(err)
	}
	detail = adminGet[platformadmin.BusinessDetailResponse](t, app, "/api/v1/admin/businesses/"+first.ID.String(), actor.ClerkUserID, 200)
	if detail.Subscription == nil || detail.Subscription.Tier != "essential" || detail.Subscription.StripeCustomerID == nil || *detail.Subscription.StripeCustomerID != "cus_private" {
		t.Fatalf("subscription=%+v", detail.Subscription)
	}
	if a := detail.Modules["appointments"]; !a.Owned || !a.CanAcceptNew || a.Usage != 1 || a.Limit != 50 {
		t.Fatalf("appointments=%+v", a)
	}
	if a := detail.Modules["preorders"]; a.Owned || a.Enabled || a.Usage != 0 {
		t.Fatalf("preorders=%+v", a)
	}
	adminGet[any](t, app, "/api/v1/admin/businesses/"+uuid.NewString(), actor.ClerkUserID, 404)
	adminGet[any](t, app, "/api/v1/admin/businesses/invalid", actor.ClerkUserID, 400)
	action := "platform_admin.test_" + suffix
	for i := 0; i < 3; i++ {
		if err := api.AuditWithMetadata(q, ctx, first.ID, actor.ID, action, "business", first.ID, map[string]any{"index": i}); err != nil {
			t.Fatal(err)
		}
	}
	filter := "/api/v1/admin/activity?business_id=" + first.ID.String() + "&actor_user_id=" + actor.ID.String() + "&action=" + action + "&limit=2"
	activity := adminGet[api.CursorPage[platformadmin.ActivityResponse]](t, app, filter, actor.ClerkUserID, 200)
	if len(activity.Items) != 2 || activity.NextCursor == nil {
		t.Fatalf("activity=%+v", activity)
	}
	activityNext := adminGet[api.CursorPage[platformadmin.ActivityResponse]](t, app, filter+"&cursor="+*activity.NextCursor, actor.ClerkUserID, 200)
	if len(activityNext.Items) != 1 || activityNext.NextCursor != nil {
		t.Fatalf("activity second=%+v", activityNext)
	}
	seen := map[uuid.UUID]bool{}
	for _, item := range append(activity.Items, activityNext.Items...) {
		if seen[item.ID] {
			t.Fatal("duplicate activity across pages")
		}
		seen[item.ID] = true
		if item.BusinessID == nil || *item.BusinessID != first.ID || item.ActorUserID == nil || *item.ActorUserID != actor.ID || len(item.Metadata) == 0 {
			t.Fatalf("activity=%+v", item)
		}
	}
	empty := adminGet[api.CursorPage[platformadmin.ActivityResponse]](t, app, "/api/v1/admin/activity?business_id="+second.ID.String(), actor.ClerkUserID, 200)
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatalf("empty=%+v", empty)
	}
	// Non-allowlisted users must not even be upserted during denied inspection.
	denied := "user_denied_" + uuid.NewString()
	adminGet[any](t, app, "/api/v1/admin/businesses/"+first.ID.String(), denied, 403)
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM users WHERE clerk_user_id=$1", denied).Scan(&count); err != nil || count != 0 {
		t.Fatalf("denied actor was persisted: count=%d err=%v", count, err)
	}

}

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
	if err := api.AuditWithMetadata(db.New(nested), ctx, business.ID, actor.ID, "platform_admin.test", "business", entity, map[string]any{"before": map[string]any{"tier": "free"}, "after": map[string]any{"tier": "essential"}, "reason": "private pilot"}); err != nil {
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
