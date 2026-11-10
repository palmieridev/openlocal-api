package operations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
	"github.com/palmieridev/openlocal-api/internal/operations/operationspostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type writeState struct{ Customers, Appointments, Preorders, Items, History, Access, Replay, Usage, Jobs int }

func (f fixture) state(t *testing.T) writeState {
	t.Helper()
	var s writeState
	must(t, f.h.RT.Pool.QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM module_customers WHERE business_id=$1),
 (SELECT count(*) FROM appointments WHERE business_id=$1),
 (SELECT count(*) FROM preorders WHERE business_id=$1),
 (SELECT count(*) FROM preorder_items WHERE business_id=$1),
 (SELECT count(*) FROM request_history WHERE business_id=$1),
 (SELECT count(*) FROM request_access WHERE business_id=$1),
 (SELECT count(*) FROM request_idempotency WHERE business_id=$1),
 (SELECT count(*) FROM module_usage_events WHERE business_id=$1),
 (SELECT count(*) FROM pgmq.q_openlocal_jobs WHERE message->>'business_id'=$1::text)`, f.b.ID).Scan(&s.Customers, &s.Appointments, &s.Preorders, &s.Items, &s.History, &s.Access, &s.Replay, &s.Usage, &s.Jobs))
	return s
}
func (f fixture) appointmentInput(phone string) operations.AppointmentInput {
	return operations.AppointmentInput{CustomerInput: operations.CustomerInput{CustomerName: "Test Customer", CustomerPhone: phone}, ServiceID: f.svc.ID, ResourceID: f.resource.ID, StartsAt: f.start}
}
func TestForeignAndMissingCreateReferencesRejectWithoutWrites(t *testing.T) {
	f, other := setup(t), setup(t)
	ctx := context.Background()
	foreignWindow, err := other.h.RT.Q.UpsertPickupWindow(ctx, db.UpsertPickupWindowParams{BusinessID: other.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
	must(t, err)
	localWindow, err := f.h.RT.Q.UpsertPickupWindow(ctx, db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
	must(t, err)
	for _, test := range []struct {
		name, module string
		foreign      uuid.UUID
		body         func(uuid.UUID) any
	}{
		{"service", "appointments", other.svc.ID, func(id uuid.UUID) any { in := f.appointmentInput("+525555558101"); in.ServiceID = id; return in }},
		{"resource", "appointments", other.resource.ID, func(id uuid.UUID) any { in := f.appointmentInput("+525555558102"); in.ResourceID = id; return in }},
		{"window", "preorders", foreignWindow.ID, func(id uuid.UUID) any {
			return map[string]any{"customer_name": "Test Customer", "customer_phone": "+525555558103", "pickup_window_id": id, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1", "unit_price": "50"}}}
		}},
		{"variant", "preorders", other.variant.ID, func(id uuid.UUID) any {
			return map[string]any{"customer_name": "Test Customer", "customer_phone": "+525555558104", "pickup_window_id": localWindow.ID, "items": []map[string]any{{"variant_id": id, "quantity": "1", "unit_price": "50"}}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := f.state(t)
			var previous []byte
			var status int
			for _, id := range []uuid.UUID{test.foreign, uuid.New()} {
				code, data := f.request("POST", "/public/businesses/"+f.b.Slug+"/"+test.module, uuid.NewString(), test.body(id), false)
				if code < 400 || code >= 500 {
					t.Fatalf("reference accepted or internal error: %d %s", code, data)
				}
				if previous != nil && (status != code || !bytes.Equal(previous, data)) {
					t.Fatalf("foreign/missing resources distinguishable: %d %s vs %d %s", status, previous, code, data)
				}
				previous, status = data, code
				if after := f.state(t); after != before {
					t.Fatalf("rejection wrote state: before %+v after %+v", before, after)
				}
			}
		})
	}
}
func TestPreorderItemFailureRollsBackEarlierEdits(t *testing.T) {
	f, other := setup(t), setup(t)
	create := func(f fixture) operations.Record {
		t.Helper()
		w, e := f.h.RT.Q.UpsertPickupWindow(context.Background(), db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
		must(t, e)
		code, data := f.request("POST", "/public/businesses/"+f.b.Slug+"/preorders", uuid.NewString(), map[string]any{"customer_name": "Test Customer", "customer_phone": "+525555558201", "pickup_window_id": w.ID, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1", "unit_price": "50"}}}, false)
		if code != 201 {
			t.Fatalf("create %d %s", code, data)
		}
		var r operations.Record
		must(t, json.Unmarshal(data, &r))
		return r
	}
	a, b := create(f), create(other)
	before := f.state(t)
	for _, id := range []uuid.UUID{b.Items[0].ID, uuid.New()} {
		code, data := f.request("PATCH", "/businesses/"+f.b.ID.String()+"/preorders/"+a.ID.String(), uuid.NewString(), map[string]any{"version": 1, "notes": "must roll back", "items": []map[string]any{{"id": a.Items[0].ID, "final_quantity": "2"}, {"id": id, "final_quantity": "3"}}, "final_total": "100"}, true)
		if code != 400 {
			t.Fatalf("foreign/missing item %d %s", code, data)
		}
		code, data = f.request("GET", "/businesses/"+f.b.ID.String()+"/preorders/"+a.ID.String(), "", nil, true)
		var stored operations.Record
		must(t, json.Unmarshal(data, &stored))
		if code != 200 || stored.Version != 1 || stored.Notes != "" || stored.Items[0].FinalQuantity != nil || stored.FinalTotal != nil {
			t.Fatalf("partial item edit persisted: %d %s", code, data)
		}
		if after := f.state(t); before != after {
			t.Fatalf("partial side effects: %+v vs %+v", before, after)
		}
	}
}
func TestAccessExtensionCannotChangeAnotherBusinessCredential(t *testing.T) {
	f, other := setup(t), setup(t)
	r := f.appointment(t, "+525555558301", f.start)
	var before time.Time
	must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT expires_at FROM request_access WHERE public_id=$1", r.ID).Scan(&before))
	store := operationspostgres.New(f.h.RT.Pool, f.h.RT.Q)
	must(t, store.ExtendRequestAccess(context.Background(), application.AccessExtension{BusinessID: other.b.ID, PublicID: r.ID, ExpiresAt: before.Add(24 * time.Hour)}))
	var after time.Time
	must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT expires_at FROM request_access WHERE public_id=$1", r.ID).Scan(&after))
	if !before.Equal(after) {
		t.Fatal("foreign business extended another credential")
	}
}
func TestReschedulePreservesSnapshotAndStaleExpirationIsHarmless(t *testing.T) {
	f := setup(t)
	r := f.appointment(t, "+525555558401", f.start)
	_, err := f.h.RT.Pool.Exec(context.Background(), "UPDATE services SET duration_minutes=60,buffer_minutes=15,price=999 WHERE id=$1", f.svc.ID)
	must(t, err)
	next := f.start.AddDate(0, 0, 2)
	path := "/businesses/" + f.b.ID.String() + "/appointments/" + r.ID.String()
	code, data := f.request("PATCH", path, uuid.NewString(), map[string]any{"version": 1, "starts_at": next}, true)
	if code != 200 {
		t.Fatalf("reschedule %d %s", code, data)
	}
	var edited operations.Record
	must(t, json.Unmarshal(data, &edited))
	if edited.DurationMinutes != 30 || edited.EndsAt.Sub(edited.StartsAt) != 30*time.Minute || edited.Price != nil || !edited.ExpiresAt.After(r.ExpiresAt) {
		t.Fatalf("snapshot or expiration changed incorrectly: %s", data)
	}
	must(t, operations.NewService(f.h.RT, f.h.Config).Expire(context.Background(), f.b.ID, r.ID, "appointments"))
	code, data = f.request("POST", path+"/transitions", uuid.NewString(), operations.TransitionInput{Version: 2, Status: "confirmed"}, true)
	if code != 200 {
		t.Fatalf("confirmation after stale expiration %d %s", code, data)
	}
	must(t, json.Unmarshal(data, &edited))
	if edited.DurationMinutes != 30 || edited.EndsAt.Sub(edited.StartsAt) != 30*time.Minute || len(edited.History) != 2 {
		t.Fatalf("confirmation snapshot/history: %s", data)
	}
}

func TestAuthorizationResolutionPrecedesBodyValidation(t *testing.T) {
	f := setup(t)
	r := f.appointment(t, "+525555558701", f.start)
	for _, path := range []string{"/public/requests/" + r.ID.String() + "/cancel", "/public/businesses/missing-" + uuid.NewString() + "/appointments"} {
		code, data := f.request("POST", path, uuid.NewString(), map[string]any{"unknown": "invalid body"}, false)
		if code != 404 {
			t.Fatalf("unauthorized or missing resource decoded body: %d %s", code, data)
		}
	}
	other := f
	other.user = "other-body-" + uuid.NewString()
	other.org = ""
	t.Cleanup(func() { must(t, f.h.RT.Q.DeleteUserByClerkID(context.Background(), other.user)) })
	code, data := other.request("POST", "/me/activity/appointments/"+r.ID.String()+"/cancel", uuid.NewString(), map[string]any{"unknown": "invalid body"}, true)
	if code != 404 {
		t.Fatalf("foreign personal request decoded body: %d %s", code, data)
	}
}
