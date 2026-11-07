package operations

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

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/accessgrants"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type grantFixture struct {
	fixture
	admin string
}

func setupGrants(t *testing.T) grantFixture {
	f := setup(t)
	admin := "user_platform_" + uuid.NewString()
	accessgrants.NewHandler(f.h.RT, []string{admin}).RegisterRoutes(f.app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
	_, err := f.h.RT.Pool.Exec(context.Background(), "UPDATE business_subscriptions SET tier='free',status='inactive',modules='{}' WHERE business_id=$1", f.b.ID)
	must(t, err)
	t.Cleanup(func() {
		_, _ = f.h.RT.Pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE business_id=$1", f.b.ID)
	})
	return grantFixture{f, admin}
}
func (f grantFixture) adminRequest(method, path string, body any) (int, []byte) {
	req := httptest.NewRequest(method, "/api/v1/admin/businesses/"+f.b.ID.String()+"/grants"+path, bytes.NewReader(requestBody(body)))
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
func (f grantFixture) grant(t *testing.T, module, tier string, end *time.Time) accessgrants.Grant {
	t.Helper()
	code, data := f.adminRequest("POST", "", accessgrants.CreateInput{Module: module, Tier: tier, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: end, Indefinite: end == nil, InternalReason: "private gift", OwnerMessage: ptr("On us")})
	if code != 201 {
		t.Fatalf("grant %d %s", code, data)
	}
	var g accessgrants.Grant
	must(t, json.Unmarshal(data, &g))
	return g
}
func ptr(s string) *string { return &s }
func (f grantFixture) access(t *testing.T, role string) entitlements.Result {
	t.Helper()
	s, err := f.h.RT.Q.GetSubscription(context.Background(), f.b.ID)
	must(t, err)
	a, err := entitlements.Get(context.Background(), f.h.RT.Q, s, role, time.Now())
	must(t, err)
	return a
}
func (f grantFixture) team(t *testing.T, role string) fixture {
	t.Helper()
	other := f.fixture
	other.user = "user_team_" + uuid.NewString()
	u, err := f.h.RT.Q.UpsertUserFromClerk(context.Background(), db.UpsertUserFromClerkParams{ClerkUserID: other.user})
	must(t, err)
	_, err = f.h.RT.Q.AddBusinessMember(context.Background(), db.AddBusinessMemberParams{BusinessID: f.b.ID, UserID: u.ID, ClerkOrgID: f.org, Role: role})
	must(t, err)
	return other
}
func TestGrantOnlyIntakeConfirmationAndHistory(t *testing.T) {
	f := setupGrants(t)
	g := f.grant(t, "appointments", "essential", nil)
	if a := f.access(t, "owner"); a.Modules["appointments"].Limit != 50 || a.Modules["preorders"].Owned {
		t.Fatalf("%+v", a)
	}
	// Complimentary access works through the real public route and owner confirmation.
	r := f.appointment(t, "+525555551001", f.start)
	path := "/businesses/" + f.b.ID.String() + "/appointments/" + r.ID.String() + "/transitions"
	code, data := f.request("POST", path, uuid.NewString(), TransitionInput{Status: "confirmed", Version: 1}, true)
	if code != 200 {
		t.Fatalf("confirm %d %s", code, data)
	}
	// Revocation removes only access, preserving history and existing record operations.
	code, data = f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "gift ended"})
	if code != 200 {
		t.Fatalf("revoke %d %s", code, data)
	}
	code, data = f.request("POST", path, uuid.NewString(), TransitionInput{Status: "completed", Version: 2}, true)
	if code != 200 {
		t.Fatalf("complete after revoke %d %s", code, data)
	}
	code, data = f.request("GET", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String(), "", nil, true)
	if code != 200 {
		t.Fatalf("read history %d %s", code, data)
	}
	var out Record
	must(t, json.Unmarshal(data, &out))
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
	f := setupGrants(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_subscriptions SET tier='essential',status='active',modules=ARRAY['appointments'] WHERE business_id=$1", f.b.ID)
	must(t, err)
	g := f.grant(t, "preorders", "business", nil)
	a := f.access(t, "manager")
	if a.Modules["appointments"].Tier != "essential" || a.Modules["appointments"].CanConfigure || !a.Modules["preorders"].CanConfigure || !a.Modules["preorders"].CanExport {
		t.Fatalf("mixed %+v", a)
	}
	manager := f.team(t, "manager")
	staff := f.team(t, "staff")
	base := "/businesses/" + f.b.ID.String()
	checks := []struct {
		f            fixture
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
		{f.fixture, "POST", base + "/appointment-resources", map[string]any{"name": "Extra"}, 409},
		{f.fixture, "PATCH", base + "/services/" + f.svc.ID.String(), map[string]any{"buffer_minutes": 5}, 403},
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
	f := setupGrants(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_modules SET enabled=false WHERE business_id=$1", f.b.ID)
	must(t, err)
	g := f.grant(t, "appointments", "business", nil)
	if a := f.access(t, "owner"); !a.Modules["appointments"].Owned || a.Modules["appointments"].Enabled || a.Modules["appointments"].CanAcceptNew {
		t.Fatalf("grant auto-enabled %+v", a)
	}
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE business_modules SET enabled=true WHERE business_id=$1", f.b.ID)
	must(t, err)
	for _, state := range []string{"scheduled", "expired"} {
		var sql string
		if state == "scheduled" {
			sql = "UPDATE business_access_grants SET starts_at=now()+interval '1 day' WHERE id=$1"
		} else {
			sql = "UPDATE business_access_grants SET starts_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE id=$1"
		}
		_, err = f.h.RT.Pool.Exec(ctx, sql, g.ID)
		must(t, err)
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
	f := setupGrants(t)
	ctx := context.Background()
	end := time.Now().Add(time.Hour)
	g := f.grant(t, "appointments", "essential", &end)
	f.grant(t, "appointments", "essential", nil)
	// Two overlapping grants still allow 50 confirmations total. Preload 49 genuine usage events.
	for i := 0; i < 49; i++ {
		must(t, f.h.RT.Q.ConsumeModuleUsage(ctx, db.ConsumeModuleUsageParams{BusinessID: f.b.ID, Module: "appointments", RecordID: uuid.New(), PeriodStart: TS(time.Now().AddDate(0, -1, 0))}))
	}
	a := f.appointment(t, "+525555551101", f.start)
	b := f.appointment(t, "+525555551102", f.start.Add(time.Hour))
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, r := range []Record{a, b} {
		wg.Add(1)
		go func(r Record) {
			defer wg.Done()
			code, _ := f.request("POST", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String()+"/transitions", uuid.NewString(), TransitionInput{Status: "confirmed", Version: 1}, true)
			codes <- code
		}(r)
	}
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
func TestGrantEditsVersionsHistoryAndAuditRollback(t *testing.T) {
	f := setupGrants(t)
	ctx := context.Background()
	end := time.Now().Add(time.Hour)
	g := f.grant(t, "appointments", "business", &end)
	later := end.Add(time.Hour)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := f.adminRequest("POST", "/"+g.ID.String()+"/extend", accessgrants.ExtendInput{Version: 1, ExpiresAt: &later, InternalReason: "extension"})
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	results := map[int]int{}
	for code := range codes {
		results[code]++
	}
	if results[200] != 1 || results[409] != 1 {
		t.Fatalf("edit race %v", results)
	}
	// A business-specific constraint forces the audit insert to fail; mutations must roll back.
	constraint := "grant_audit_" + stringsUUID()
	_, err := f.h.RT.Pool.Exec(ctx, fmt.Sprintf("ALTER TABLE audit_logs ADD CONSTRAINT %s CHECK (business_id <> '%s'::uuid OR entity_type <> 'access_grant') NOT VALID", constraint, f.b.ID))
	must(t, err)
	defer func() { _, _ = f.h.RT.Pool.Exec(ctx, "ALTER TABLE audit_logs DROP CONSTRAINT "+constraint) }()
	code, data := f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 2, InternalReason: "rollback test"})
	if code != 500 {
		t.Fatalf("audit rejection %d %s", code, data)
	}

	evenLater := later.Add(time.Hour)
	code, data = f.adminRequest("POST", "/"+g.ID.String()+"/extend", accessgrants.ExtendInput{Version: 2, ExpiresAt: &evenLater, InternalReason: "rollback extension"})
	if code != 500 {
		t.Fatalf("extension audit rejection %d %s", code, data)
	}
	stored, err := f.h.RT.Q.GetAccessGrant(ctx, db.GetAccessGrantParams{BusinessID: f.b.ID, ID: g.ID})
	must(t, err)
	if stored.RevokedAt.Valid || stored.Version != 2 || !stored.ExpiresAt.Time.Equal(later.Truncate(time.Microsecond)) {
		t.Fatalf("audit failed to rollback %+v", stored)
	}
	code, _ = f.adminRequest("POST", "", accessgrants.CreateInput{Module: "preorders", Tier: "essential", StartsAt: time.Now(), Indefinite: true, InternalReason: "rollback create"})
	if code != 500 {
		t.Fatalf("create audit rejection %d", code)
	}
	count, err := f.h.RT.Q.CountAccessGrants(ctx, f.b.ID)
	must(t, err)
	if count != 1 {
		t.Fatal("create survived audit failure")
	}
	_, err = f.h.RT.Pool.Exec(ctx, "ALTER TABLE audit_logs DROP CONSTRAINT "+constraint)
	must(t, err)
	code, data = f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 2, InternalReason: "done"})
	if code != 200 {
		t.Fatalf("revoke %d %s", code, data)
	}
	code, data = f.adminRequest("GET", "/"+g.ID.String()+"/history", nil)
	if code != 200 {
		t.Fatalf("history %d %s", code, data)
	}
	var history struct {
		Items []accessgrants.History `json:"items"`
	}
	must(t, json.Unmarshal(data, &history))
	if len(history.Items) != 3 {
		t.Fatalf("history %s", data)
	}
	for i, item := range history.Items {
		var metadata struct {
			InternalReason string `json:"internal_reason"`
			Before, After  *accessgrants.Grant
		}
		must(t, json.Unmarshal(item.Metadata, &metadata))
		if item.ActorUserID == nil || metadata.InternalReason == "" || metadata.After.Version != int32(i+1) || i > 0 && metadata.Before.Version != int32(i) {
			t.Fatalf("history %s", data)
		}
	}
}
func stringsUUID() string { u := uuid.New(); return fmt.Sprintf("%x", u[:]) }

// Ensure a revoked grant committed while holding the operational lock is visible to confirmation.
func TestGrantRevocationSerializesWithConfirmation(t *testing.T) {
	f := setupGrants(t)
	ctx := context.Background()
	g := f.grant(t, "appointments", "business", nil)
	r := f.appointment(t, "+525555551201", f.start)
	tx, err := f.h.RT.Pool.Begin(ctx)
	must(t, err)
	defer tx.Rollback(ctx)
	q := f.h.RT.Q.WithTx(tx)
	_, err = q.LockSubscription(ctx, f.b.ID)
	must(t, err)
	actor, err := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: f.admin})
	must(t, err)
	_, err = q.RevokeAccessGrant(ctx, db.RevokeAccessGrantParams{BusinessID: f.b.ID, ID: g.ID, Version: 1, RevokedBy: uuid.NullUUID{UUID: actor.ID, Valid: true}, RevocationReason: sql.NullString{String: "concurrent revoke", Valid: true}})
	must(t, err)
	started := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		close(started)
		code, _ := f.request("POST", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String()+"/transitions", uuid.NewString(), TransitionInput{Status: "confirmed", Version: 1}, true)
		done <- code
	}()
	<-started
	select {
	case code := <-done:
		t.Fatalf("confirmation bypassed lock: %d", code)
	case <-time.After(30 * time.Millisecond):
	}
	must(t, tx.Commit(ctx))
	if code := <-done; code != 403 {
		t.Fatalf("confirmation after revoke %d", code)
	}
	if access := f.access(t, "owner"); access.Modules["appointments"].Usage != 0 {
		t.Fatal("rejected confirmation consumed quota")
	}
}

func TestGrantFallbackRestrictsIntakeResourcesAndAdvancedSettings(t *testing.T) {
	f := setupGrants(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_subscriptions SET tier='essential',status='active',modules=ARRAY['appointments','preorders'] WHERE business_id=$1", f.b.ID)
	must(t, err)
	g := f.grant(t, "appointments", "business", nil)
	extra, err := f.h.RT.Q.SaveAppointmentResource(ctx, db.SaveAppointmentResourceParams{ID: uuid.New(), BusinessID: f.b.ID, Name: "ZZZ extra", Active: true, ServiceIds: []uuid.UUID{}, Hours: []byte("[]"), ClosedDates: []byte("[]")})
	must(t, err)
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE services SET buffer_minutes=10 WHERE id=$1", f.svc.ID)
	must(t, err)
	code, data := f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "fallback"})
	if code != 200 {
		t.Fatalf("revoke %d %s", code, data)
	}
	svc, err := f.h.RT.Q.GetService(ctx, db.GetServiceParams{BusinessID: f.b.ID, ID: f.svc.ID})
	must(t, err)
	slots, err := appointmentSlots(ctx, f.h.RT.Q, f.b, f.cfg, svc, f.start.Format("2006-01-02"), extra.ID, uuid.Nil)
	must(t, err)
	if len(slots) != 0 {
		t.Fatal("extra resource still contributes to Essential intake")
	}
	r := f.appointment(t, "+525555551301", f.start)
	stored, err := f.h.RT.Q.GetAppointment(ctx, db.GetAppointmentParams{BusinessID: f.b.ID, ID: r.ID})
	must(t, err)
	if stored.BufferMinutes != 0 {
		t.Fatal("advanced buffer still contributes to Essential intake")
	}
	sub, err := f.h.RT.Q.GetSubscription(ctx, f.b.ID)
	must(t, err)
	cfg, err := requireIntake(ctx, f.h.RT.Q, sub, "preorders")
	must(t, err)
	if cfg.MaxOrdersPerSlot != 0 || cfg.DailyCutoff != "" {
		t.Fatal("advanced preorder settings active on Essential")
	}
	saved, err := settings(ctx, f.h.RT.Q, f.b.ID, "preorders")
	must(t, err)
	if saved.MaxOrdersPerSlot != 1 {
		t.Fatal("downgrade rewrote saved settings")
	}
	resources, err := f.h.RT.Q.ListAppointmentResources(ctx, f.b.ID)
	must(t, err)
	if len(resources) != 2 || !resources[1].Active {
		t.Fatal("fallback deleted or disabled saved resource")
	}
}

func TestEssentialCanEditBasicFieldsWhileRetainingBusinessConfiguration(t *testing.T) {
	f := setupGrants(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_subscriptions SET tier='essential',status='active',modules=ARRAY['appointments','preorders'] WHERE business_id=$1", f.b.ID)
	must(t, err)
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE services SET buffer_minutes=10 WHERE id=$1", f.svc.ID)
	must(t, err)
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE appointment_resources SET service_ids=ARRAY[$1]::uuid[] WHERE id=$2", f.svc.ID, f.resource.ID)
	must(t, err)
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
	saved, err := settings(ctx, f.h.RT.Q, f.b.ID, "preorders")
	must(t, err)
	if saved.MaxOrdersPerSlot != 1 {
		t.Fatal("basic edit erased advanced settings")
	}
}

func TestGrantAPIRevocationWaitsForOperationalLock(t *testing.T) {
	f := setupGrants(t)
	g := f.grant(t, "appointments", "business", nil)
	ctx := context.Background()
	tx, err := f.h.RT.Pool.Begin(ctx)
	must(t, err)
	defer tx.Rollback(ctx)
	_, err = f.h.RT.Q.WithTx(tx).LockSubscription(ctx, f.b.ID)
	must(t, err)
	done := make(chan int, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		code, _ := f.adminRequest("POST", "/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "serialized revoke"})
		done <- code
	}()
	<-started
	select {
	case code := <-done:
		t.Fatalf("grant API bypassed operational lock: %d", code)
	case <-time.After(30 * time.Millisecond):
	}
	must(t, tx.Commit(ctx))
	if code := <-done; code != 200 {
		t.Fatalf("revoke after unlock %d", code)
	}
}
