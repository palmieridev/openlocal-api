package operations_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
	"github.com/shopspring/decimal"
)

type fixture struct {
	h         *operations.Handler
	app       *fiber.App
	b         db.Business
	svc       db.Service
	resource  db.AppointmentResource
	variant   db.ProductVariant
	user, org string
	cfg       operations.Settings
	start     time.Time
}

func setup(t *testing.T) fixture {
	t.Helper()
	dsn := os.Getenv("OPENLOCAL_OPERATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicit migrated disposable OPENLOCAL_OPERATIONS_TEST_DATABASE_URL")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(parsed.ConnConfig.Database, "openlocal_operations_test") || (parsed.ConnConfig.Host != "localhost" && parsed.ConnConfig.Host != "127.0.0.1" && !strings.HasPrefix(parsed.ConnConfig.Host, "/")) {
		t.Fatal("operations tests require a disposable local openlocal_operations_test database")
	}
	ctx := context.Background()
	// The widest contention test needs four writers, the held transaction,
	// and a connection observing their locks, even on small CI runners.
	parsed.MaxConns = 6
	pool, e := pgxpool.NewWithConfig(ctx, parsed)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	rt := api.NewRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), pool, nil)
	q := rt.Q
	org := "org_test_" + uuid.NewString()
	user := "user_test_" + uuid.NewString()
	u, e := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: user})
	must(t, e)
	b, e := q.CreateBusiness(ctx, db.CreateBusinessParams{ClerkOrgID: sql.NullString{String: org, Valid: true}, Name: "Module test", Slug: "module-" + uuid.NewString(), BusinessType: "servicios", Status: "active", Timezone: "America/Mexico_City", LocationMode: "fixed"})
	must(t, e)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, statement := range []string{
			"DELETE FROM pgmq.q_openlocal_jobs WHERE message->>'business_id'=$1",
			"DELETE FROM pgmq.a_openlocal_jobs WHERE message->>'business_id'=$1",
			"DELETE FROM pgmq.q_openlocal_failed WHERE message->>'business_id'=$1",
		} {
			if _, err := pool.Exec(cleanupCtx, statement, b.ID.String()); err != nil {
				t.Error(err)
			}
		}
		for _, statement := range []string{
			"DELETE FROM request_history WHERE business_id=$1",
			"DELETE FROM request_access WHERE business_id=$1",
			"DELETE FROM module_usage_events WHERE business_id=$1",
			"DELETE FROM preorder_items WHERE business_id=$1",
			"DELETE FROM preorders WHERE business_id=$1",
			"DELETE FROM preorder_pickup_windows WHERE business_id=$1",
			"DELETE FROM appointments WHERE business_id=$1",
			"DELETE FROM appointment_resources WHERE business_id=$1",
			"DELETE FROM services WHERE business_id=$1",
			"DELETE FROM module_customers WHERE business_id=$1",
			"DELETE FROM businesses WHERE id=$1",
		} {
			if _, err := pool.Exec(cleanupCtx, statement, b.ID); err != nil {
				t.Error(err)
			}
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id=$1", u.ID); err != nil {
			t.Error(err)
		}
	})
	_, e = q.AddBusinessMember(ctx, db.AddBusinessMemberParams{BusinessID: b.ID, UserID: u.ID, ClerkOrgID: org, Role: "owner"})
	must(t, e)
	must(t, q.EnsureSubscription(ctx, b.ID))
	_, e = q.UpdateSubscription(ctx, db.UpdateSubscriptionParams{BusinessID: b.ID, Tier: "business", Status: "pilot", Modules: []string{"appointments", "preorders"}, PeriodStart: operations.TS(time.Now().Add(-time.Hour)), PeriodEnd: operations.TS(time.Now().AddDate(0, 1, 0))})
	must(t, e)
	cfg := operations.Settings{Hours: []scheduling.Hour{}, ClosedDates: []string{}, LeadMinutes: 60, HorizonDays: 30, SlotMinutes: 30, CancelBeforeMinutes: 60, PendingTTLMinutes: 1440, VariantRules: []operations.VariantRule{}}
	cfg.Enabled = true
	cfg.MaxOrdersPerSlot = 1
	for d := 0; d < 7; d++ {
		cfg.Hours = append(cfg.Hours, scheduling.Hour{DayOfWeek: d, OpensAt: "09:00", ClosesAt: "18:00"})
	}
	svc, e := q.SaveService(ctx, db.SaveServiceParams{ID: uuid.New(), BusinessID: b.ID, Name: "Consulta", DurationMinutes: 30, IsPublic: true, Active: true})
	must(t, e)
	resource, e := q.SaveAppointmentResource(ctx, db.SaveAppointmentResourceParams{ID: uuid.New(), BusinessID: b.ID, Name: "Principal", Active: true, ServiceIds: []uuid.UUID{}, Hours: []byte("[]"), ClosedDates: []byte("[]")})
	must(t, e)
	product, e := q.CreateProduct(ctx, db.CreateProductParams{BusinessID: b.ID, Name: "Pollo", Slug: "pollo", Unit: "kg", ProductType: "stocked_product", IsPublic: true, Status: "active"})
	must(t, e)
	variant, e := q.CreateVariant(ctx, db.CreateVariantParams{BusinessID: b.ID, ProductID: product.ID, Sku: uuid.NewString(), InternalCode: uuid.NewString(), Name: "Entero", Attributes: []byte("{}"), Price: decimal.NewFromInt(50), Currency: "MXN", TrackInventory: true, PublicStockStatus: "available", IsPublic: true, Status: "active"})
	must(t, e)
	cfg.VariantRules = []operations.VariantRule{{VariantID: variant.ID, MinQuantity: "0.5", MaxQuantity: "10", Step: "0.5"}}
	for _, m := range []string{"appointments", "preorders"} {
		_, e = q.UpsertModule(ctx, db.UpsertModuleParams{BusinessID: b.ID, Module: m, Enabled: true, Settings: jsonBytes(cfg)})
		must(t, e)
	}
	h := operations.NewHandler(rt, config.Config{AppEnv: "development", ModulesAllowTestChallenge: true, ModulesTokenKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	h.RegisterPublicRoutes(app.Group("/api/v1"))
	h.RegisterPrivateRoutes(app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
	loc, _ := time.LoadLocation(b.Timezone)
	tomorrow := time.Now().In(loc).AddDate(0, 0, 1)
	start := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 10, 0, 0, 0, loc)
	return fixture{h, app, b, svc, resource, variant, user, org, cfg, start}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func (f fixture) request(method, path, key string, body any, owner bool) (int, []byte) {
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(requestBody(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	if owner {
		req.Header.Set("X-Test-Clerk-User-ID", f.user)
		req.Header.Set("X-Test-Clerk-Org-ID", f.org)
		req.Header.Set("X-Test-Clerk-Org-Role", "org:admin")
	}
	res, e := f.app.Test(req, -1)
	if e != nil {
		return 0, []byte(e.Error())
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}
func (f fixture) appointment(t *testing.T, phone string, start time.Time) operations.Record {
	t.Helper()
	code, data := f.request("POST", "/public/businesses/"+f.b.Slug+"/appointments", uuid.NewString(), map[string]any{"customer_name": "Test Customer", "customer_phone": phone, "service_id": f.svc.ID, "resource_id": f.resource.ID, "starts_at": start}, false)
	if code != 201 {
		t.Fatalf("create: %d %s", code, data)
	}
	var r operations.Record
	must(t, json.Unmarshal(data, &r))
	return r
}
func TestConcurrentConfirmationAndReplay(t *testing.T) {
	f := setup(t)
	a := f.appointment(t, "+525555550001", f.start)
	b := f.appointment(t, "+525555550002", f.start)
	release, wait := f.holdSubscription(t)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, r := range []operations.Record{a, b} {
		wg.Add(1)
		go func(r operations.Record) {
			defer wg.Done()
			code, _ := f.request("POST", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String()+"/transitions", uuid.NewString(), operations.TransitionInput{Status: "confirmed", Version: 1}, true)
			codes <- code
		}(r)
	}
	wait(2)
	release()
	wg.Wait()
	close(codes)
	count := map[int]int{}
	for c := range codes {
		count[c]++
	}
	if count[200] != 1 || count[409] != 1 {
		t.Fatalf("concurrent results %v", count)
	}
	body := operations.AppointmentInput{CustomerInput: operations.CustomerInput{CustomerName: "Repeat Customer", CustomerPhone: "+525555550003"}, ServiceID: f.svc.ID, ResourceID: f.resource.ID, StartsAt: f.start.Add(time.Hour)}
	key := uuid.NewString()
	path := "/public/businesses/" + f.b.Slug + "/appointments"
	code, data := f.request("POST", path, key, body, false)
	if code != 201 {
		t.Fatalf("create %d %s", code, data)
	}
	code, again := f.request("POST", path, key, body, false)
	if code != 201 || !bytes.Equal(data, again) {
		t.Fatalf("replay differs %d %s", code, again)
	}
	body.CustomerName = "Changed"
	code, _ = f.request("POST", path, key, body, false)
	if code != 409 {
		t.Fatalf("changed replay = %d", code)
	}
}
func TestExpiryPreservesHistoryAndTokenIsolation(t *testing.T) {
	f := setup(t)
	r := f.appointment(t, "+525555550004", f.start)
	ctx := context.Background()
	_, e := f.h.RT.Pool.Exec(ctx, "UPDATE appointments SET expires_at=now()-interval '1 second' WHERE id=$1", r.ID)
	must(t, e)
	must(t, f.h.Expire(ctx, f.b.ID, r.ID, "appointments"))
	must(t, f.h.Expire(ctx, f.b.ID, r.ID, "appointments"))
	code, data := f.request("GET", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String(), "", nil, true)
	if code != 200 {
		t.Fatalf("read %d %s", code, data)
	}
	var out operations.Record
	must(t, json.Unmarshal(data, &out))
	if out.Status != "expired" || len(out.History) != 2 {
		t.Fatalf("expiry/history: %+v", out)
	}
	for _, token := range []string{"wrong", r.ManagementToken} {
		req := httptest.NewRequest("GET", "/api/v1/public/requests/"+r.ID.String(), nil)
		req.Header.Set("X-Openlocal-Management-Token", token)
		res, e := f.app.Test(req)
		must(t, e)
		_ = res.Body.Close()
		want := 404
		if token == r.ManagementToken {
			want = 200
		}
		if res.StatusCode != want {
			t.Fatalf("access %d want %d", res.StatusCode, want)
		}
	}
}
func TestPreorderCapacityPriceAndNoStockMutation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w, e := f.h.RT.Q.UpsertPickupWindow(ctx, db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
	must(t, e)
	body := map[string]any{"customer_name": "Test Customer", "customer_phone": "+525555550010", "pickup_window_id": w.ID, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1.5", "unit_price": "50.00"}}}
	path := "/public/businesses/" + f.b.Slug + "/preorders"
	code, data := f.request("POST", path, uuid.NewString(), body, false)
	if code != 201 {
		t.Fatalf("preorder %d %s", code, data)
	}
	var a operations.Record
	must(t, json.Unmarshal(data, &a))
	if a.EstimatedTotal != "75.00" {
		t.Fatal(a.EstimatedTotal)
	}
	body["customer_phone"] = "+525555550011"
	code, data = f.request("POST", path, uuid.NewString(), body, false)
	if code != 201 {
		t.Fatalf("preorder %d %s", code, data)
	}
	var b operations.Record
	must(t, json.Unmarshal(data, &b))
	for i, r := range []operations.Record{a, b} {
		code, data = f.request("POST", "/businesses/"+f.b.ID.String()+"/preorders/"+r.ID.String()+"/transitions", uuid.NewString(), operations.TransitionInput{Status: "confirmed", Version: 1}, true)
		want := 200
		if i == 1 {
			want = 409
		}
		if code != want {
			t.Fatalf("capacity %d %s", code, data)
		}
	}
	var movements int
	must(t, f.h.RT.Pool.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE business_id=$1", f.b.ID).Scan(&movements))
	if movements != 0 {
		t.Fatal("preorders changed stock")
	}
	body["items"] = []map[string]any{{"variant_id": f.variant.ID, "quantity": "1.5", "unit_price": "1.00"}}
	w, e = f.h.RT.Q.UpsertPickupWindow(ctx, db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start.Add(time.Hour)), EndsAt: operations.TS(f.start.Add(90 * time.Minute))})
	must(t, e)
	body["pickup_window_id"] = w.ID
	code, _ = f.request("POST", path, uuid.NewString(), body, false)
	if code != 409 {
		t.Fatalf("stale price %d", code)
	}
}

func requestBody(body any) []byte {
	if body == nil {
		return nil
	}
	return jsonBytes(body)
}

func jsonBytes(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
