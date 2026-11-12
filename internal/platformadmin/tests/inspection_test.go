package platformadmin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/palmieridev/openlocal-api/internal/audit"
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
	pool := adminPool(t)
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
	for _, query := range []string{first.ID.String(), first.Slug, "  " + first.Slug + "  ", orgID, "ADMIN %_ " + suffix} {
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
	shape := adminGet[map[string]any](t, app, "/api/v1/admin/businesses/"+first.ID.String(), actor.ClerkUserID, 200)
	if shape["subscription"] != nil || shape["access_role"] != "owner" {
		t.Fatalf("detail shape=%v", shape)
	}
	if err := q.EnsureSubscription(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	free := adminGet[map[string]any](t, app, "/api/v1/admin/businesses/"+first.ID.String(), actor.ClerkUserID, 200)["subscription"].(map[string]any)
	if m, ok := free["modules"].([]any); !ok || len(m) != 0 {
		t.Fatalf("modules=%v", free["modules"])
	}
	for _, key := range []string{"stripe_customer_id", "stripe_subscription_id", "grace_until", "scheduled_change"} {
		if value, ok := free[key]; !ok || value != nil {
			t.Fatalf("%s=%v", key, value)
		}
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
		if err := audit.WithMetadata(q, ctx, first.ID, actor.ID, action, "business", first.ID, map[string]any{"index": i}); err != nil {
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

func TestAllInspectionRoutesDenyOrdinaryUsers(t *testing.T) {
	for _, allowed := range [][]string{nil, {"user_platform"}} {
		app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
		platformadmin.NewHandler(api.Runtime{}, config.Config{PlatformAdminUserIDs: allowed}).RegisterPrivateRoutes(app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
		for _, path := range []string{"/admin/businesses?q=private", "/admin/businesses/invalid", "/admin/activity?actor_user_id=invalid"} {
			for _, user := range []string{"", "user_ordinary_owner"} {
				req := httptest.NewRequest("GET", "/api/v1"+path, nil)
				req.Header.Set("X-Test-Clerk-User-ID", user)
				req.Header.Set("X-Test-Clerk-Org-ID", "org_1")
				req.Header.Set("X-Test-Clerk-Org-Role", "org:admin")
				res, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()
				want := 403
				if user == "" {
					want = 401
				}
				if res.StatusCode != want {
					t.Fatalf("%s status=%d", path, res.StatusCode)
				}
				if user != "" && res.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("denial may be cached")
				}
			}
		}
	}
}

func TestInspectionWithoutSubscriptionResolvesGrantAndUTCUsageWithoutWrites(t *testing.T) {
	pool := adminPool(t)
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	q := db.New(tx)
	actor := createUser(t, ctx, q)
	business := createBusiness(t, ctx, q)
	app := adminTestApp(q, actor.ClerkUserID)
	if _, err = q.CreateAccessGrant(ctx, db.CreateAccessGrantParams{BusinessID: business.ID, Module: "appointments", Tier: "essential", StartsAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}, InternalReason: "private reason", CreatedBy: uuid.NullUUID{UUID: actor.ID, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err = q.UpsertModule(ctx, db.UpsertModuleParams{BusinessID: business.ID, Module: "appointments", Enabled: true, Settings: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err = q.ConsumeModuleUsage(ctx, db.ConsumeModuleUsageParams{BusinessID: business.ID, Module: "appointments", RecordID: uuid.New(), PeriodStart: pgtype.Timestamptz{Time: time.Now().AddDate(0, -1, 0), Valid: true}}); err != nil {
		t.Fatal(err)
	}
	detail := adminGet[platformadmin.BusinessDetailResponse](t, app, "/api/v1/admin/businesses/"+business.ID.String(), actor.ClerkUserID, 200)
	a := detail.Modules["appointments"]
	if detail.Subscription != nil || !a.Owned || !a.CanAcceptNew || a.Tier != "essential" || a.Usage != 1 || len(a.Sources) != 1 || a.Sources[0].Kind != "grant" {
		t.Fatalf("detail=%+v module=%+v", detail, a)
	}
	if _, err = q.GetSubscription(ctx, business.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("inspection wrote subscription: %v", err)
	}
	var audits int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM audit_logs WHERE business_id=$1", business.ID).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("inspection wrote audit count=%d err=%v", audits, err)
	}
	// Null foreign keys retain their JSON keys in the private activity projection.
	row, err := q.CreateAuditLog(ctx, db.CreateAuditLogParams{BusinessID: uuid.NullUUID{UUID: business.ID, Valid: true}, Action: "admin.null_projection", EntityType: "business", Metadata: []byte(`{"reason":"private"}`)})
	if err != nil {
		t.Fatal(err)
	}
	page := adminGet[api.CursorPage[map[string]any]](t, app, "/api/v1/admin/activity?business_id="+business.ID.String()+"&action=admin.null_projection", actor.ClerkUserID, 200)
	if len(page.Items) != 1 || page.Items[0]["id"] != row.ID.String() {
		t.Fatalf("activity=%+v", page)
	}
	for _, key := range []string{"actor_user_id", "entity_id"} {
		if value, ok := page.Items[0][key]; !ok || value != nil {
			t.Fatalf("%s=%v", key, value)
		}
	}
	if page.Items[0]["metadata"].(map[string]any)["reason"] != "private" {
		t.Fatalf("metadata=%v", page.Items[0]["metadata"])
	}
}
