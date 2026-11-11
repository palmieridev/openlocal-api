package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/palmieridev/openlocal-api/internal/operations"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/accessgrants"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type operationsGrantFixture struct {
	operationsFixture
	admin string
}

func setupOperationsGrants(t *testing.T) operationsGrantFixture {
	f := setupOperations(t)
	admin := "user_platform_" + uuid.NewString()
	t.Cleanup(func() { mustOperations(t, f.h.RT.Q.DeleteUserByClerkID(context.Background(), admin)) })
	accessgrants.NewHandler(f.h.RT, []string{admin}).RegisterRoutes(f.app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
	_, err := f.h.RT.Pool.Exec(context.Background(), "UPDATE business_subscriptions SET tier='free',status='inactive',modules='{}' WHERE business_id=$1", f.b.ID)
	mustOperations(t, err)
	t.Cleanup(func() {
		_, _ = f.h.RT.Pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE business_id=$1", f.b.ID)
	})
	return operationsGrantFixture{f, admin}
}
func (f operationsGrantFixture) adminRequest(method, path string, body any) (int, []byte) {
	req := httptest.NewRequest(method, "/api/v1/admin/businesses/"+f.b.ID.String()+"/grants"+path, bytes.NewReader(operationsBody(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Clerk-User-ID", f.admin)
	res, err := f.app.Test(req, -1)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}
func (f operationsGrantFixture) grant(t *testing.T, module, tier string, end *time.Time) accessgrants.Grant {
	t.Helper()
	code, data := f.adminRequest("POST", "", accessgrants.CreateInput{Module: module, Tier: tier, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: end, Indefinite: end == nil, InternalReason: "private gift", OwnerMessage: operationsPtr("On us")})
	if code != 201 {
		t.Fatalf("grant %d %s", code, data)
	}
	var g accessgrants.Grant
	mustOperations(t, json.Unmarshal(data, &g))
	return g
}
func operationsPtr(s string) *string { return &s }
func (f operationsGrantFixture) access(t *testing.T, role string) entitlements.Result {
	t.Helper()
	s, err := f.h.RT.Q.GetSubscription(context.Background(), f.b.ID)
	mustOperations(t, err)
	a, err := entitlements.Get(context.Background(), f.h.RT.Q, s, role, time.Now())
	mustOperations(t, err)
	return a
}
func (f operationsGrantFixture) team(t *testing.T, role string) operationsFixture {
	t.Helper()
	other := f.operationsFixture
	other.user = "user_team_" + uuid.NewString()
	t.Cleanup(func() { mustOperations(t, f.h.RT.Q.DeleteUserByClerkID(context.Background(), other.user)) })
	u, err := f.h.RT.Q.UpsertUserFromClerk(context.Background(), db.UpsertUserFromClerkParams{ClerkUserID: other.user})
	mustOperations(t, err)
	_, err = f.h.RT.Q.AddBusinessMember(context.Background(), db.AddBusinessMemberParams{BusinessID: f.b.ID, UserID: u.ID, ClerkOrgID: f.org, Role: role})
	mustOperations(t, err)
	return other
}
func TestGrantOnlyIntakeConfirmationAndHistory(t *testing.T) {
	f := setupOperationsGrants(t)
	g := f.grant(t, "appointments", "essential", nil)
	if a := f.access(t, "owner"); a.Modules["appointments"].Limit != 50 || a.Modules["preorders"].Owned {
		t.Fatalf("%+v", a)
	}
	// Complimentary access works through the real public route and owner confirmation.
	r := f.appointment(t, "+525555551001", f.start)
	path := "/businesses/" + f.b.ID.String() + "/appointments/" + r.ID.String() + "/transitions"
	code, data := f.request("POST", path, uuid.NewString(), operations.TransitionInput{Status: "confirmed", Version: 1}, true)
	if code != 200 {
		t.Fatalf("confirm %d %s", code, data)
	}
	// Revocation removes only access, preserving history and existing record operations.
	code, data = f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "gift ended"})
	if code != 200 {
		t.Fatalf("revoke %d %s", code, data)
	}
	code, data = f.request("POST", path, uuid.NewString(), operations.TransitionInput{Status: "completed", Version: 2}, true)
	if code != 200 {
		t.Fatalf("complete after revoke %d %s", code, data)
	}
	code, data = f.request("GET", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String(), "", nil, true)
	if code != 200 {
		t.Fatalf("read history %d %s", code, data)
	}
	var out operations.Record
	mustOperations(t, json.Unmarshal(data, &out))
	if len(out.History) != 3 {
		t.Fatalf("history %+v", out.History)
	}
	code, _ = f.request("GET", "/public/businesses/"+f.b.Slug+"/appointments/services", "", nil, false)
	if code != 403 {
		t.Fatalf("intake after revoke %d", code)
	}
	if a := f.access(t, "owner"); a.Modules["appointments"].Usage != 1 || a.Modules["appointments"].Owned {
		t.Fatalf("usage/access %+v", a)
	}
}
func TestGrantsMixedModuleCapabilitiesAndTeam(t *testing.T) {
	f := setupOperationsGrants(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_subscriptions SET tier='essential',status='active',modules=ARRAY['appointments'] WHERE business_id=$1", f.b.ID)
	mustOperations(t, err)
	g := f.grant(t, "preorders", "business", nil)
	a := f.access(t, "manager")
	if a.Modules["appointments"].Tier != "essential" || a.Modules["appointments"].CanConfigure || !a.Modules["preorders"].CanConfigure || !a.Modules["preorders"].CanExport {
		t.Fatalf("mixed %+v", a)
	}
	manager := f.team(t, "manager")
	staff := f.team(t, "staff")
	base := "/businesses/" + f.b.ID.String()
	checks := []struct {
		f            operationsFixture
		method, path string
		body         any
		want         int
	}{
		{manager, "GET", base + "/preorders", nil, 200},
		{manager, "GET", base + "/appointments", nil, 403},
		{manager, "PATCH", base + "/preorders/settings", map[string]any{"daily_cutoff": "17:00"}, 200},
		{staff, "PATCH", base + "/preorders/settings", map[string]any{"daily_cutoff": "17:00"}, 403},
		{manager, "GET", base + "/preorders/export?from=" + f.start.Format("2006-01-02") + "&to=" + f.start.Format("2006-01-02"), nil, 200},
		{staff, "GET", base + "/preorders/export?from=" + f.start.Format("2006-01-02") + "&to=" + f.start.Format("2006-01-02"), nil, 403},
		{f.operationsFixture, "POST", base + "/appointment-resources", map[string]any{"name": "Extra"}, 409},
		{f.operationsFixture, "PATCH", base + "/services/" + f.svc.ID.String(), map[string]any{"buffer_minutes": 5}, 403},
	}
	for _, c := range checks {
		code, data := c.f.request(c.method, c.path, uuid.NewString(), c.body, true)
		if code != c.want {
			t.Fatalf("%s %s: %d want %d %s", c.method, c.path, code, c.want, data)
		}
	}
	// A Business preorder grant does not expand appointment resources or advanced settings.
	ga := f.grant(t, "appointments", "business", nil)
	code, data := manager.request("POST", base+"/appointment-resources", uuid.NewString(), map[string]any{"name": "Extra"}, true)
	if code != 200 {
		t.Fatalf("Business resource %d %s", code, data)
	}
	code, data = manager.request("PATCH", base+"/services/"+f.svc.ID.String(), uuid.NewString(), map[string]any{"buffer_minutes": 5}, true)
	if code != 200 {
		t.Fatalf("Business buffer %d %s", code, data)
	}
	// After revocation, team retains record history, while paid Essential still works for owners.
	code, data = f.adminRequest("POST", "/"+ga.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "end upgrade"})
	if code != 200 {
		t.Fatalf("revoke %d %s", code, data)
	}
	code, _ = manager.request("GET", base+"/appointments", "", nil, true)
	if code != 200 {
		t.Fatalf("team history %d", code)
	}
	code, _ = manager.request("PATCH", base+"/appointments/settings", uuid.NewString(), map[string]any{"lead_minutes": 15}, true)
	if code != 403 {
		t.Fatalf("team config after downgrade %d", code)
	}
	if a = f.access(t, "owner"); a.Modules["appointments"].Tier != "essential" || a.Modules["preorders"].Tier != "business" {
		t.Fatalf("fallback %+v grant %v", a, g.ID)
	}
}
func TestGrantDoesNotEnableIntakeAndScheduledExpiredDenied(t *testing.T) {
	f := setupOperationsGrants(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_modules SET enabled=false WHERE business_id=$1", f.b.ID)
	mustOperations(t, err)
	g := f.grant(t, "appointments", "business", nil)
	if a := f.access(t, "owner"); !a.Modules["appointments"].Owned || a.Modules["appointments"].Enabled || a.Modules["appointments"].CanAcceptNew {
		t.Fatalf("grant auto-enabled %+v", a)
	}
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE business_modules SET enabled=true WHERE business_id=$1", f.b.ID)
	mustOperations(t, err)
	for _, state := range []string{"scheduled", "expired"} {
		var sql string
		if state == "scheduled" {
			sql = "UPDATE business_access_grants SET starts_at=now()+interval '1 day' WHERE id=$1"
		} else {
			sql = "UPDATE business_access_grants SET starts_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE id=$1"
		}
		_, err = f.h.RT.Pool.Exec(ctx, sql, g.ID)
		mustOperations(t, err)
		code, data := f.request("GET", "/public/businesses/"+f.b.Slug+"/appointments/services", "", nil, false)
		if code != 403 {
			t.Fatalf("%s intake %d %s", state, code, data)
		}
		code, _ = f.request("POST", "/businesses/"+f.b.ID.String()+"/appointment-resources", uuid.NewString(), map[string]any{"name": "Extra"}, true)
		if code != 403 {
			t.Fatalf("%s resource %d", state, code)
		}
	}
}
func TestGrantQuotaNoAdditionResetAndConcurrentLastSlot(t *testing.T) {
	f := setupOperationsGrants(t)
	ctx := context.Background()
	end := time.Now().Add(time.Hour)
	g := f.grant(t, "appointments", "essential", &end)
	f.grant(t, "appointments", "essential", nil)
	// Two overlapping grants still allow 50 confirmations total. Preload 49 genuine usage events.
	for i := 0; i < 49; i++ {
		mustOperations(t, f.h.RT.Q.ConsumeModuleUsage(ctx, db.ConsumeModuleUsageParams{BusinessID: f.b.ID, Module: "appointments", RecordID: uuid.New(), PeriodStart: operations.TS(time.Now().AddDate(0, -1, 0))}))
	}
	a := f.appointment(t, "+525555551101", f.start)
	b := f.appointment(t, "+525555551102", f.start.Add(time.Hour))
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
	results := map[int]int{}
	for code := range codes {
		results[code]++
	}
	if results[200] != 1 || results[409] != 1 {
		t.Fatalf("last slot %v", results)
	}
	later := end.Add(time.Hour)
	code, data := f.adminRequest("POST", "/"+g.ID.String()+"/extend", accessgrants.ExtendInput{Version: 1, ExpiresAt: &later, InternalReason: "more time"})
	if code != 200 {
		t.Fatalf("extend %d %s", code, data)
	}
	access := f.access(t, "owner")
	if access.Modules["appointments"].Usage != 50 || access.Modules["appointments"].Limit != 50 || access.Modules["appointments"].CanAcceptNew {
		t.Fatalf("extension reset %+v", access)
	}
	// A stronger grant raises the ceiling to 1000, without adding or resetting usage.
	business := f.grant(t, "appointments", "business", nil)
	if access = f.access(t, "owner"); access.Modules["appointments"].Limit != 1000 || access.Modules["appointments"].Usage != 50 {
		t.Fatalf("upgrade %+v", access)
	}
	code, _ = f.adminRequest("POST", "/"+business.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "restore"})
	if code != 200 {
		t.Fatal(code)
	}
	if access = f.access(t, "owner"); access.Modules["appointments"].Limit != 50 || access.Modules["appointments"].Usage != 50 {
		t.Fatalf("downgrade %+v", access)
	}
}
func TestGrantRevocationSerializesWithConfirmation(t *testing.T) {
	f := setupOperationsGrants(t)
	ctx := context.Background()
	g := f.grant(t, "appointments", "business", nil)
	r := f.appointment(t, "+525555551201", f.start)
	tx, err := f.h.RT.Pool.Begin(ctx)
	mustOperations(t, err)
	defer tx.Rollback(ctx)
	q := f.h.RT.Q.WithTx(tx)
	_, err = q.LockSubscription(ctx, f.b.ID)
	mustOperations(t, err)
	actor, err := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: f.admin})
	mustOperations(t, err)
	_, err = q.RevokeAccessGrant(ctx, db.RevokeAccessGrantParams{BusinessID: f.b.ID, ID: g.ID, Version: 1, RevokedBy: uuid.NullUUID{UUID: actor.ID, Valid: true}, RevocationReason: sql.NullString{String: "concurrent revoke", Valid: true}})
	mustOperations(t, err)
	started := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		close(started)
		code, _ := f.request("POST", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String()+"/transitions", uuid.NewString(), operations.TransitionInput{Status: "confirmed", Version: 1}, true)
		done <- code
	}()
	<-started
	var blocker int
	mustOperations(t, tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blocker))
	waitForOperationsBlocked(t, f.operationsFixture, blocker, 1)
	mustOperations(t, tx.Commit(ctx))
	if code := <-done; code != 403 {
		t.Fatalf("confirmation after revoke %d", code)
	}
	if access := f.access(t, "owner"); access.Modules["appointments"].Usage != 0 {
		t.Fatal("rejected confirmation consumed quota")
	}
}

func TestGrantFallbackRestrictsIntakeResourcesAndAdvancedSettings(t *testing.T) {
	f := setupOperationsGrants(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_subscriptions SET tier='essential',status='active',modules=ARRAY['appointments','preorders'] WHERE business_id=$1", f.b.ID)
	mustOperations(t, err)
	g := f.grant(t, "appointments", "business", nil)
	extra, err := f.h.RT.Q.SaveAppointmentResource(ctx, db.SaveAppointmentResourceParams{ID: uuid.New(), BusinessID: f.b.ID, Name: "ZZZ extra", Active: true, ServiceIds: []uuid.UUID{}, Hours: []byte("[]"), ClosedDates: []byte("[]")})
	mustOperations(t, err)
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE services SET buffer_minutes=10 WHERE id=$1", f.svc.ID)
	mustOperations(t, err)
	code, data := f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "fallback"})
	if code != 200 {
		t.Fatalf("revoke %d %s", code, data)
	}
	code, data = f.request("GET", "/public/businesses/"+f.b.Slug+"/appointments/availability?service_id="+f.svc.ID.String()+"&resource_id="+extra.ID.String()+"&date="+f.start.Format("2006-01-02"), "", nil, false)
	var availability struct {
		Slots []json.RawMessage `json:"slots"`
	}
	mustOperations(t, json.Unmarshal(data, &availability))
	if code != 200 || len(availability.Slots) != 0 {
		t.Fatal("extra resource still contributes to Essential intake")
	}
	r := f.appointment(t, "+525555551301", f.start)
	stored, err := f.h.RT.Q.GetAppointment(ctx, db.GetAppointmentParams{BusinessID: f.b.ID, ID: r.ID})
	mustOperations(t, err)
	if stored.BufferMinutes != 0 {
		t.Fatal("advanced buffer still contributes to Essential intake")
	}
	// Essential intake ignores the retained Business capacity limit.
	w, err := f.h.RT.Q.UpsertPickupWindow(ctx, db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
	mustOperations(t, err)
	for i := 0; i < 2; i++ {
		code, data := f.request("POST", "/public/businesses/"+f.b.Slug+"/preorders", uuid.NewString(), map[string]any{"customer_name": "Essential Customer", "customer_phone": fmt.Sprintf("+52555555900%d", i), "pickup_window_id": w.ID, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1", "unit_price": "50.00"}}}, false)
		if code != 201 {
			t.Fatalf("Essential create %d %s", code, data)
		}
		var order operations.Record
		mustOperations(t, json.Unmarshal(data, &order))
		code, data = f.request("POST", "/businesses/"+f.b.ID.String()+"/preorders/"+order.ID.String()+"/transitions", uuid.NewString(), operations.TransitionInput{Status: "confirmed", Version: 1}, true)
		if code != 200 {
			t.Fatalf("Essential ignores Business capacity %d %s", code, data)
		}
	}
	code, savedBody := f.request("GET", "/businesses/"+f.b.ID.String()+"/preorders/settings", "", nil, true)
	if code != 200 {
		t.Fatalf("settings %d %s", code, savedBody)
	}
	var saved operations.Settings
	mustOperations(t, json.Unmarshal(savedBody, &saved))
	if saved.MaxOrdersPerSlot != 1 {
		t.Fatal("downgrade rewrote saved settings")
	}
	resources, err := f.h.RT.Q.ListAppointmentResources(ctx, f.b.ID)
	mustOperations(t, err)
	if len(resources) != 2 || !resources[1].Active {
		t.Fatal("fallback deleted or disabled saved resource")
	}
}

func TestGrantFallbackAllowsResourceEditsWithoutIncreasingCapacity(t *testing.T) {
	for _, state := range []string{"expired", "revoked"} {
		t.Run(state, func(t *testing.T) {
			f := setupOperationsGrants(t)
			ctx := context.Background()
			_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_subscriptions SET tier='essential',status='active',modules=ARRAY['appointments'] WHERE business_id=$1", f.b.ID)
			mustOperations(t, err)
			g := f.grant(t, "appointments", "business", nil)
			base := "/businesses/" + f.b.ID.String() + "/appointment-resources"
			createResource := func(name string, active bool) operations.Resource {
				t.Helper()
				code, data := f.request("POST", base, uuid.NewString(), map[string]any{"name": name, "active": active}, true)
				if code != 200 {
					t.Fatalf("create resource %d %s", code, data)
				}
				var resource operations.Resource
				mustOperations(t, json.Unmarshal(data, &resource))
				return resource
			}
			extra := createResource("ZZZ extra", true)
			inactive := createResource("ZZZ inactive", false)
			if state == "expired" {
				_, err = f.h.RT.Pool.Exec(ctx, "UPDATE business_access_grants SET expires_at=$1 WHERE id=$2", time.Now().Add(-time.Minute), g.ID)
				mustOperations(t, err)
			} else {
				code, data := f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "fallback"})
				if code != 200 {
					t.Fatalf("revoke %d %s", code, data)
				}
			}
			if access := f.access(t, "owner"); access.Modules["appointments"].MaxResources != 1 {
				t.Fatalf("expected Essential fallback: %+v", access)
			}
			for _, resource := range []operations.Resource{{ID: f.resource.ID, Name: f.resource.Name}, extra} {
				code, data := f.request("PATCH", base+"/"+resource.ID.String(), uuid.NewString(), map[string]any{"name": "Renamed " + resource.Name, "active": true}, true)
				if code != 200 {
					t.Fatalf("edit retained active resource %d %s", code, data)
				}
				var saved operations.Resource
				mustOperations(t, json.Unmarshal(data, &saved))
				if saved.Name != "Renamed "+resource.Name || !saved.Active {
					t.Fatalf("basic edit was not saved: %+v", saved)
				}
			}
			for _, c := range []struct {
				method string
				path   string
				body   any
			}{
				{"POST", base, map[string]any{"name": "New active"}},
				{"PATCH", base + "/" + inactive.ID.String(), map[string]any{"active": true}},
			} {
				code, data := f.request(c.method, c.path, uuid.NewString(), c.body, true)
				if code != 409 {
					t.Fatalf("capacity increase %s %d %s", c.method, code, data)
				}
			}
			for _, id := range []uuid.UUID{extra.ID, f.resource.ID} {
				code, data := f.request("PATCH", base+"/"+id.String(), uuid.NewString(), map[string]any{"active": false}, true)
				if code != 200 {
					t.Fatalf("deactivate retained resource %d %s", code, data)
				}
			}
			code, data := f.request("PATCH", base+"/"+inactive.ID.String(), uuid.NewString(), map[string]any{"active": true}, true)
			if code != 200 {
				t.Fatalf("activate within Essential limit %d %s", code, data)
			}
		})
	}
}

func TestEssentialCanEditBasicFieldsWhileRetainingBusinessConfiguration(t *testing.T) {
	f := setupOperationsGrants(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_subscriptions SET tier='essential',status='active',modules=ARRAY['appointments','preorders'] WHERE business_id=$1", f.b.ID)
	mustOperations(t, err)
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE services SET buffer_minutes=10 WHERE id=$1", f.svc.ID)
	mustOperations(t, err)
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE appointment_resources SET service_ids=ARRAY[$1]::uuid[] WHERE id=$2", f.svc.ID, f.resource.ID)
	mustOperations(t, err)
	base := "/businesses/" + f.b.ID.String()
	for _, c := range []struct {
		path string
		body any
		want int
	}{
		{base + "/preorders/settings", map[string]any{"enabled": false}, 200},
		{base + "/preorders/settings", map[string]any{"max_orders_per_slot": 2}, 403},
		{base + "/services/" + f.svc.ID.String(), map[string]any{"name": "Renamed service"}, 200},
		{base + "/services/" + f.svc.ID.String(), map[string]any{"buffer_minutes": 15}, 403},
		{base + "/appointment-resources/" + f.resource.ID.String(), map[string]any{"active": false}, 200},
	} {
		code, data := f.request("PATCH", c.path, uuid.NewString(), c.body, true)
		if code != c.want {
			t.Fatalf("%s %d want %d %s", c.path, code, c.want, data)
		}
	}
	code, savedBody := f.request("GET", "/businesses/"+f.b.ID.String()+"/preorders/settings", "", nil, true)
	if code != 200 {
		t.Fatalf("settings %d %s", code, savedBody)
	}
	var saved operations.Settings
	mustOperations(t, json.Unmarshal(savedBody, &saved))
	if saved.MaxOrdersPerSlot != 1 {
		t.Fatal("basic edit erased advanced settings")
	}
}

func TestGrantAPIRevocationWaitsForOperationalLock(t *testing.T) {
	f := setupOperationsGrants(t)
	g := f.grant(t, "appointments", "business", nil)
	ctx := context.Background()
	tx, err := f.h.RT.Pool.Begin(ctx)
	mustOperations(t, err)
	defer tx.Rollback(ctx)
	_, err = f.h.RT.Q.WithTx(tx).LockSubscription(ctx, f.b.ID)
	mustOperations(t, err)
	done := make(chan int, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		code, _ := f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "serialized revoke"})
		done <- code
	}()
	<-started
	var blocker int
	mustOperations(t, tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blocker))
	waitForOperationsBlocked(t, f.operationsFixture, blocker, 1)
	mustOperations(t, tx.Commit(ctx))
	if code := <-done; code != 200 {
		t.Fatalf("revoke after unlock %d", code)
	}
}
