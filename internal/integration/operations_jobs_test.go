package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/jobs"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestOperationExpirationQueueDeliveryAndDuplicate(t *testing.T) {
	f := setupOperations(t)
	ctx := context.Background()
	r := f.appointment(t, "+525555557101", f.start)
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE appointments SET expires_at=now()-interval '1 second' WHERE id=$1", r.ID)
	mustOperations(t, err)
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE pgmq.q_openlocal_jobs SET vt=now() WHERE message->>'business_id'=$1", f.b.ID.String())
	mustOperations(t, err)
	worker := jobs.Worker{Q: f.h.RT.Q, Operations: operations.NewService(f.h.RT, f.h.Config)}
	mustOperations(t, worker.Process(ctx))
	// At-least-once delivery must not append another expiration or increment version.
	_, err = f.h.RT.Q.EnqueueJob(ctx, db.EnqueueJobParams{Payload: operationsJSON(jobs.Payload{Type: "expire_request", BusinessID: f.b.ID, RecordID: r.ID, Module: "appointments"})})
	mustOperations(t, err)
	mustOperations(t, worker.Process(ctx))
	var status string
	var version, history, queued, archived int
	mustOperations(t, f.h.RT.Pool.QueryRow(ctx, `SELECT status,version,
 (SELECT count(*) FROM request_history WHERE appointment_id=$1),
 (SELECT count(*) FROM pgmq.q_openlocal_jobs WHERE message->>'business_id'=$2),
 (SELECT count(*) FROM pgmq.a_openlocal_jobs WHERE message->>'business_id'=$2)
 FROM appointments WHERE id=$1`, r.ID, f.b.ID.String()).Scan(&status, &version, &history, &queued, &archived))
	if status != "expired" || version != 2 || history != 2 || queued != 0 || archived != 2 {
		t.Fatalf("queue expiration: %s v%d history=%d queued=%d archived=%d", status, version, history, queued, archived)
	}
}
func TestOperationSweepRecoversMissingPreorderJob(t *testing.T) {
	f := setupOperations(t)
	ctx := context.Background()
	w, err := f.h.RT.Q.UpsertPickupWindow(ctx, db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
	mustOperations(t, err)
	code, data := f.request("POST", "/public/businesses/"+f.b.Slug+"/preorders", uuid.NewString(), map[string]any{"customer_name": "Queue Customer", "customer_phone": "+525555557102", "pickup_window_id": w.ID, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1", "unit_price": "50"}}}, false)
	if code != 201 {
		t.Fatalf("preorder %d %s", code, data)
	}
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE preorders SET expires_at=now()-interval '1 second' WHERE business_id=$1", f.b.ID)
	mustOperations(t, err)
	_, err = f.h.RT.Pool.Exec(ctx, "DELETE FROM pgmq.q_openlocal_jobs WHERE message->>'business_id'=$1", f.b.ID.String())
	mustOperations(t, err)
	worker := jobs.Worker{Q: f.h.RT.Q, Operations: operations.NewService(f.h.RT, f.h.Config)}
	mustOperations(t, worker.Sweep(ctx))
	mustOperations(t, worker.Sweep(ctx))
	var status string
	var version, history int
	mustOperations(t, f.h.RT.Pool.QueryRow(ctx, "SELECT status,version,(SELECT count(*) FROM request_history WHERE business_id=$1) FROM preorders WHERE business_id=$1", f.b.ID).Scan(&status, &version, &history))
	if status != "expired" || version != 2 || history != 2 {
		t.Fatalf("recovery sweep: %s v%d history=%d", status, version, history)
	}
}

type failExpiration struct{}

func (failExpiration) Expire(context.Context, uuid.UUID, uuid.UUID, string) error {
	return errors.New("transient application failure")
}
func TestOperationQueueRetriesAndArchivesFailure(t *testing.T) {
	f := setupOperations(t)
	ctx := context.Background()
	r := f.appointment(t, "+525555557103", f.start)
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE pgmq.q_openlocal_jobs SET vt=now() WHERE message->>'business_id'=$1", f.b.ID.String())
	mustOperations(t, err)
	worker := jobs.Worker{Q: f.h.RT.Q, Operations: failExpiration{}}
	mustOperations(t, worker.Process(ctx))
	var reads int
	var deferred bool
	mustOperations(t, f.h.RT.Pool.QueryRow(ctx, "SELECT read_ct,vt>now() FROM pgmq.q_openlocal_jobs WHERE message->>'business_id'=$1", f.b.ID.String()).Scan(&reads, &deferred))
	if reads != 1 || !deferred {
		t.Fatalf("failed delivery not deferred: reads=%d deferred=%v", reads, deferred)
	}
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE pgmq.q_openlocal_jobs SET vt=now(),read_ct=7 WHERE message->>'business_id'=$1", f.b.ID.String())
	mustOperations(t, err)
	mustOperations(t, worker.Process(ctx))
	var active, failed, history int
	var status string
	mustOperations(t, f.h.RT.Pool.QueryRow(ctx, `SELECT status,
 (SELECT count(*) FROM pgmq.q_openlocal_jobs WHERE message->>'business_id'=$2),
 (SELECT count(*) FROM pgmq.q_openlocal_failed WHERE message->>'business_id'=$2),
 (SELECT count(*) FROM request_history WHERE appointment_id=$1)
 FROM appointments WHERE id=$1`, r.ID, f.b.ID.String()).Scan(&status, &active, &failed, &history))
	if status != "requested" || active != 0 || failed != 1 || history != 1 {
		t.Fatalf("failed job changed operation or was lost: %s active=%d failed=%d history=%d", status, active, failed, history)
	}
}
