package operations_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func (f fixture) rejectWrites(t *testing.T, table string) {
	t.Helper()
	ddl, err := os.ReadFile("testdata/reject_writes.sql")
	must(t, err)
	_, err = f.h.RT.Pool.Exec(context.Background(), string(ddl))
	must(t, err)
	t.Cleanup(func() {
		_, err := f.h.RT.Pool.Exec(context.Background(), "DROP FUNCTION reject_operations_fixture_write() CASCADE")
		if err != nil {
			t.Error(err)
		}
	})
	_, err = f.h.RT.Pool.Exec(context.Background(), "UPDATE businesses SET description=$2 WHERE id=$1", f.b.ID, "operations-fail:"+table)
	must(t, err)
}
func TestCreationCommitsCustomerRecordHistoryAccessReplayAndQueueTogether(t *testing.T) {
	for _, module := range []string{"appointments", "preorders"} {
		for _, table := range []string{"request_history", "request_access", "request_idempotency", "q_openlocal_jobs", "preorder_items"} {
			if module == "appointments" && table == "preorder_items" {
				continue
			}
			t.Run(module+"/"+table, func(t *testing.T) {
				f := setup(t)
				f.rejectWrites(t, table)
				var body any = f.appointmentInput("+525555558601")
				if module == "preorders" {
					w, err := f.h.RT.Q.UpsertPickupWindow(context.Background(), db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
					must(t, err)
					body = map[string]any{"customer_name": "Test Customer", "customer_phone": "+525555558601", "pickup_window_id": w.ID, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1", "unit_price": "50"}}}
				}
				before := f.state(t)
				key := uuid.NewString()
				path := "/public/businesses/" + f.b.Slug + "/" + module
				code, data := f.request("POST", path, key, body, false)
				if code != 500 {
					t.Fatalf("injected failure %d %s", code, data)
				}
				if got := f.state(t); got != before {
					t.Fatalf("partial creation: before %+v after %+v", before, got)
				}
				_, err := f.h.RT.Pool.Exec(context.Background(), "UPDATE businesses SET description='' WHERE id=$1", f.b.ID)
				must(t, err)
				code, data = f.request("POST", path, key, body, false)
				if code != 201 {
					t.Fatalf("retry after rollback %d %s", code, data)
				}
				expected := writeState{Customers: 1, History: 1, Access: 1, Replay: 1, Jobs: 1}
				if module == "appointments" {
					expected.Appointments = 1
				} else {
					expected.Preorders = 1
					expected.Items = 1
				}
				if got := f.state(t); got != expected {
					t.Fatalf("successful retry state: %+v", got)
				}
			})
		}
	}
}
func TestConfirmationFailureRollsBackUsageHistoryAndVersion(t *testing.T) {
	for _, table := range []string{"request_history", "request_idempotency"} {
		t.Run(table, func(t *testing.T) {
			f := setup(t)
			r := f.appointment(t, "+525555558602", f.start)
			f.rejectWrites(t, table)
			before := f.state(t)
			key := uuid.NewString()
			path := "/businesses/" + f.b.ID.String() + "/appointments/" + r.ID.String() + "/transitions"
			code, data := f.request("POST", path, key, operations.TransitionInput{Version: 1, Status: "confirmed"}, true)
			if code != 500 {
				t.Fatalf("confirmation failure %d %s", code, data)
			}
			if after := f.state(t); after != before {
				t.Fatalf("failed confirmation wrote side effects: %+v vs %+v", before, after)
			}
			code, data = f.request("GET", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String(), "", nil, true)
			var stored operations.Record
			must(t, json.Unmarshal(data, &stored))
			if code != 200 || stored.Version != 1 || stored.Status != "requested" {
				t.Fatalf("partial operation update: %d %s", code, data)
			}
			_, err := f.h.RT.Pool.Exec(context.Background(), "UPDATE businesses SET description='' WHERE id=$1", f.b.ID)
			must(t, err)
			code, data = f.request("POST", path, key, operations.TransitionInput{Version: 1, Status: "confirmed"}, true)
			if code != 200 {
				t.Fatalf("retry failed %d %s", code, data)
			}
			if got := f.state(t); got.Usage != 1 || got.History != 2 || got.Replay != 2 {
				t.Fatalf("confirmation retry state: %+v", got)
			}
		})
	}
}
func TestExpirationHistoryFailureRollsBackStatus(t *testing.T) {
	f := setup(t)
	r := f.appointment(t, "+525555558603", f.start)
	f.rejectWrites(t, "request_history")
	_, err := f.h.RT.Pool.Exec(context.Background(), "UPDATE appointments SET expires_at=now()-interval '1 second' WHERE id=$1", r.ID)
	must(t, err)
	service := operations.NewService(f.h.RT, f.h.Config)
	if err := service.Expire(context.Background(), f.b.ID, r.ID, "appointments"); err == nil {
		t.Fatal("expiration ignored history failure")
	}
	var status string
	var version int
	must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT status,version FROM appointments WHERE id=$1", r.ID).Scan(&status, &version))
	if status != "requested" || version != 1 {
		t.Fatalf("partial expiry: %s v%d", status, version)
	}
}
