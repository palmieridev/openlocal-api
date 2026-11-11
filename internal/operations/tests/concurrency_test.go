package operations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

// Observe actual lock contention, including waiters queued behind other waiters.
// The held transaction is released only after every operation reaches this overlap.
func (f fixture) holdSubscription(t *testing.T) (release func(), wait func(int)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	tx, err := f.h.RT.Pool.Begin(ctx)
	must(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()); cancel() })
	_, err = f.h.RT.Q.WithTx(tx).LockSubscription(ctx, f.b.ID)
	must(t, err)
	var pid int
	must(t, tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid))
	return func() { t.Helper(); must(t, tx.Commit(ctx)) }, func(n int) { t.Helper(); waitForBlocked(t, f, pid, n) }
}
func waitForBlocked(t *testing.T, f fixture, pid, n int) {
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
		must(t, err)
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
func TestConcurrentReplayCommitsOneOperationAndOneJob(t *testing.T) {
	f := setup(t)
	release, wait := f.holdSubscription(t)
	key := uuid.NewString()
	body := f.appointmentInput("+525555558501")
	type response struct {
		code int
		data []byte
	}
	done := make(chan response, 2)
	for range 2 {
		go func() {
			code, data := f.request("POST", "/public/businesses/"+f.b.Slug+"/appointments", key, body, false)
			done <- response{code, data}
		}()
	}
	wait(2)
	release()
	results := []response{}
	for range 2 {
		select {
		case r := <-done:
			results = append(results, r)
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent retry did not complete")
		}
	}
	if results[0].code != 201 || results[1].code != 201 || !bytes.Equal(results[0].data, results[1].data) {
		t.Fatalf("replay responses differ: %+v", results)
	}
	want := writeState{Customers: 1, Appointments: 1, History: 1, Access: 1, Replay: 1, Jobs: 1}
	if got := f.state(t); got != want {
		t.Fatalf("duplicate retry writes: %+v", got)
	}
	var r operations.Record
	must(t, json.Unmarshal(results[0].data, &r))
	var encrypted []byte
	must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT response FROM request_idempotency WHERE business_id=$1", f.b.ID).Scan(&encrypted))
	if bytes.Contains(encrypted, []byte(r.ManagementToken)) || bytes.Contains(encrypted, []byte(body.CustomerName)) {
		t.Fatal("replay persisted plaintext credentials/customer data")
	}
}
func TestConcurrentExpirationAppendsHistoryOnce(t *testing.T) {
	f := setup(t)
	r := f.appointment(t, "+525555558502", f.start)
	_, err := f.h.RT.Pool.Exec(context.Background(), "UPDATE appointments SET expires_at=now()-interval '1 second' WHERE id=$1", r.ID)
	must(t, err)
	release, wait := f.holdSubscription(t)
	done := make(chan error, 2)
	service := operations.NewService(f.h.RT, f.h.Config)
	for range 2 {
		go func() { done <- service.Expire(context.Background(), f.b.ID, r.ID, "appointments") }()
	}
	wait(2)
	release()
	for range 2 {
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("expiration did not complete")
		}
	}
	var status string
	var version, history int
	must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT status,version,(SELECT count(*) FROM request_history WHERE appointment_id=$1) FROM appointments WHERE id=$1", r.ID).Scan(&status, &version, &history))
	if status != "expired" || version != 2 || history != 2 {
		t.Fatalf("expiry applied more than once: %s %d %d", status, version, history)
	}
}

func TestConcurrentPendingPhoneLimitRejectsFourthWrite(t *testing.T) {
	f := setup(t)
	release, wait := f.holdSubscription(t)
	done := make(chan int, 4)
	for range 4 {
		go func() {
			code, _ := f.request("POST", "/public/businesses/"+f.b.Slug+"/appointments", uuid.NewString(), f.appointmentInput("+525555558503"), false)
			done <- code
		}()
	}
	wait(4)
	release()
	codes := map[int]int{}
	for range 4 {
		select {
		case code := <-done:
			codes[code]++
		case <-time.After(10 * time.Second):
			t.Fatal("pending-limit writes did not complete")
		}
	}
	if codes[201] != 3 || codes[409] != 1 {
		t.Fatalf("concurrent pending limit: %v", codes)
	}
	if got := f.state(t); got != (writeState{Customers: 1, Appointments: 3, History: 3, Access: 3, Replay: 3, Jobs: 3}) {
		t.Fatalf("rate-limited request wrote partial state: %+v", got)
	}
}
func TestConcurrentPreorderCapacityConsumesOneConfirmation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	window, err := f.h.RT.Q.UpsertPickupWindow(ctx, db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
	must(t, err)
	records := []operations.Record{}
	for _, phone := range []string{"+525555558504", "+525555558505"} {
		code, data := f.request("POST", "/public/businesses/"+f.b.Slug+"/preorders", uuid.NewString(), map[string]any{"customer_name": "Capacity Customer", "customer_phone": phone, "pickup_window_id": window.ID, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1", "unit_price": "50"}}}, false)
		if code != 201 {
			t.Fatalf("create %d %s", code, data)
		}
		var r operations.Record
		must(t, json.Unmarshal(data, &r))
		records = append(records, r)
	}
	release, wait := f.holdSubscription(t)
	done := make(chan int, 2)
	for _, r := range records {
		go func() {
			code, _ := f.request("POST", "/businesses/"+f.b.ID.String()+"/preorders/"+r.ID.String()+"/transitions", uuid.NewString(), operations.TransitionInput{Status: "confirmed", Version: 1}, true)
			done <- code
		}()
	}
	wait(2)
	release()
	codes := map[int]int{}
	for range 2 {
		select {
		case code := <-done:
			codes[code]++
		case <-time.After(10 * time.Second):
			t.Fatal("capacity confirmations did not complete")
		}
	}
	if codes[200] != 1 || codes[409] != 1 {
		t.Fatalf("concurrent capacity: %v", codes)
	}
	if got := f.state(t); got.Usage != 1 || got.History != 3 || got.Replay != 3 {
		t.Fatalf("capacity rejection wrote state: %+v", got)
	}
	var confirmed int
	must(t, f.h.RT.Pool.QueryRow(ctx, "SELECT count(*) FROM preorders WHERE business_id=$1 AND status='confirmed'", f.b.ID).Scan(&confirmed))
	if confirmed != 1 {
		t.Fatalf("capacity exceeded: %d", confirmed)
	}
}
func TestConcurrentEditsRejectStaleVersionWithoutSideEffects(t *testing.T) {
	f := setup(t)
	r := f.appointment(t, "+525555558506", f.start)
	release, wait := f.holdSubscription(t)
	done := make(chan int, 2)
	for _, notes := range []string{"First edit", "Second edit"} {
		go func() {
			code, _ := f.request("PATCH", "/businesses/"+f.b.ID.String()+"/appointments/"+r.ID.String(), uuid.NewString(), map[string]any{"version": 1, "notes": notes}, true)
			done <- code
		}()
	}
	wait(2)
	release()
	codes := map[int]int{}
	for range 2 {
		select {
		case code := <-done:
			codes[code]++
		case <-time.After(10 * time.Second):
			t.Fatal("edits did not complete")
		}
	}
	if codes[200] != 1 || codes[409] != 1 {
		t.Fatalf("concurrent versions: %v", codes)
	}
	if got := f.state(t); got != (writeState{Customers: 1, Appointments: 1, History: 1, Access: 1, Replay: 2, Jobs: 1}) {
		t.Fatalf("stale edit wrote state: %+v", got)
	}
	var version int
	must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT version FROM appointments WHERE id=$1", r.ID).Scan(&version))
	if version != 2 {
		t.Fatalf("stale edit advanced version: %d", version)
	}
}
