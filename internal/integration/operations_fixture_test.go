package integration_test

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

type operationsFixture struct {
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

func setupOperations(t *testing.T) operationsFixture {
	t.Helper()
	if os.Getenv("OPENLOCAL_INTEGRATION_TESTS") != "1" {
		t.Skip("requires explicit OPENLOCAL_INTEGRATION_TESTS=1")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicit migrated disposable DATABASE_URL")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(parsed.ConnConfig.Database, "openlocal_movement_test") || (parsed.ConnConfig.Host != "localhost" && parsed.ConnConfig.Host != "127.0.0.1" && !strings.HasPrefix(parsed.ConnConfig.Host, "/")) {
		t.Fatal("operations tests require a disposable local openlocal_movement_test database")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	rt := api.NewRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), pool, nil)
	q := rt.Q
	org := "org_test_" + uuid.NewString()
	user := "user_test_" + uuid.NewString()
	u, e := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: user})
	mustOperations(t, e)
	b, e := q.CreateBusiness(ctx, db.CreateBusinessParams{ClerkOrgID: sql.NullString{String: org, Valid: true}, Name: "Module test", Slug: "module-" + uuid.NewString(), BusinessType: "servicios", Status: "active", Timezone: "America/Mexico_City", LocationMode: "fixed"})
	mustOperations(t, e)
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
	mustOperations(t, e)
	mustOperations(t, q.EnsureSubscription(ctx, b.ID))
	_, e = q.UpdateSubscription(ctx, db.UpdateSubscriptionParams{BusinessID: b.ID, Tier: "business", Status: "pilot", Modules: []string{"appointments", "preorders"}, PeriodStart: operations.TS(time.Now().Add(-time.Hour)), PeriodEnd: operations.TS(time.Now().AddDate(0, 1, 0))})
	mustOperations(t, e)
	cfg := operations.Settings{Hours: []scheduling.Hour{}, ClosedDates: []string{}, LeadMinutes: 60, HorizonDays: 30, SlotMinutes: 30, CancelBeforeMinutes: 60, PendingTTLMinutes: 1440, VariantRules: []operations.VariantRule{}}
	cfg.Enabled = true
	cfg.MaxOrdersPerSlot = 1
	for d := 0; d < 7; d++ {
		cfg.Hours = append(cfg.Hours, scheduling.Hour{DayOfWeek: d, OpensAt: "09:00", ClosesAt: "18:00"})
	}
	svc, e := q.SaveService(ctx, db.SaveServiceParams{ID: uuid.New(), BusinessID: b.ID, Name: "Consulta", DurationMinutes: 30, IsPublic: true, Active: true})
	mustOperations(t, e)
	resource, e := q.SaveAppointmentResource(ctx, db.SaveAppointmentResourceParams{ID: uuid.New(), BusinessID: b.ID, Name: "Principal", Active: true, ServiceIds: []uuid.UUID{}, Hours: []byte("[]"), ClosedDates: []byte("[]")})
	mustOperations(t, e)
	product, e := q.CreateProduct(ctx, db.CreateProductParams{BusinessID: b.ID, Name: "Pollo", Slug: "pollo", Unit: "kg", ProductType: "stocked_product", IsPublic: true, Status: "active"})
	mustOperations(t, e)
	variant, e := q.CreateVariant(ctx, db.CreateVariantParams{BusinessID: b.ID, ProductID: product.ID, Sku: uuid.NewString(), InternalCode: uuid.NewString(), Name: "Entero", Attributes: []byte("{}"), Price: decimal.NewFromInt(50), Currency: "MXN", TrackInventory: true, PublicStockStatus: "available", IsPublic: true, Status: "active"})
	mustOperations(t, e)
	cfg.VariantRules = []operations.VariantRule{{VariantID: variant.ID, MinQuantity: "0.5", MaxQuantity: "10", Step: "0.5"}}
	for _, m := range []string{"appointments", "preorders"} {
		_, e = q.UpsertModule(ctx, db.UpsertModuleParams{BusinessID: b.ID, Module: m, Enabled: true, Settings: operationsJSON(cfg)})
		mustOperations(t, e)
	}
	h := operations.NewHandler(rt, config.Config{AppEnv: "development", ModulesAllowTestChallenge: true, ModulesTokenKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	h.RegisterPublicRoutes(app.Group("/api/v1"))
	h.RegisterPrivateRoutes(app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
	loc, _ := time.LoadLocation(b.Timezone)
	tomorrow := time.Now().In(loc).AddDate(0, 0, 1)
	start := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 10, 0, 0, 0, loc)
	return operationsFixture{h, app, b, svc, resource, variant, user, org, cfg, start}
}
func mustOperations(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func (f operationsFixture) request(method, path, key string, body any, owner bool) (int, []byte) {
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(operationsBody(body)))
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
func (f operationsFixture) appointment(t *testing.T, phone string, start time.Time) operations.Record {
	t.Helper()
	code, data := f.request("POST", "/public/businesses/"+f.b.Slug+"/appointments", uuid.NewString(), map[string]any{"customer_name": "Test Customer", "customer_phone": phone, "service_id": f.svc.ID, "resource_id": f.resource.ID, "starts_at": start}, false)
	if code != 201 {
		t.Fatalf("create: %d %s", code, data)
	}
	var r operations.Record
	mustOperations(t, json.Unmarshal(data, &r))
	return r
}
func operationsBody(body any) []byte {
	if body == nil {
		return nil
	}
	return operationsJSON(body)
}

func operationsJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

// Observe actual lock contention, including waiters queued behind other waiters.
// The held transaction is released only after every operation reaches this overlap.
func (f operationsFixture) holdSubscription(t *testing.T) (release func(), wait func(int)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	tx, err := f.h.RT.Pool.Begin(ctx)
	mustOperations(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()); cancel() })
	_, err = f.h.RT.Q.WithTx(tx).LockSubscription(ctx, f.b.ID)
	mustOperations(t, err)
	var pid int
	mustOperations(t, tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid))
	return func() { t.Helper(); mustOperations(t, tx.Commit(ctx)) }, func(n int) { t.Helper(); waitForOperationsBlocked(t, f, pid, n) }
}
func waitForOperationsBlocked(t *testing.T, f operationsFixture, pid, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		err := f.h.RT.Pool.QueryRow(ctx, `WITH RECURSIVE blocked(pid) AS (
   SELECT $1::integer UNION
   SELECT a.pid FROM pg_stat_activity a JOIN blocked b ON b.pid=ANY(pg_blocking_pids(a.pid))
   WHERE a.datname=current_database() AND a.wait_event_type='Lock'
  ) SELECT count(*)-1 FROM blocked`, pid).Scan(&count)
		mustOperations(t, err)
		if count >= n {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("only %d/%d operations reached the held database lock", count, n)
		case <-ticker.C:
		}
	}
}
