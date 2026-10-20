// Package jobs processes PGMQ envelopes. Business records are never queue payloads
// or deleted by queue maintenance; jobs contain only routing identifiers.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"log/slog"
	"time"
)

type Payload struct {
	Type       string    `json:"type"`
	BusinessID uuid.UUID `json:"business_id,omitempty"`
	RecordID   uuid.UUID `json:"record_id,omitempty"`
	Module     string    `json:"module,omitempty"`
	CustomerID string    `json:"customer_id,omitempty"`
}
type message struct {
	ID        int64           `json:"msg_id"`
	ReadCount int             `json:"read_ct"`
	Payload   json.RawMessage `json:"message"`
}
type Worker struct {
	Q          *db.Queries
	Operations *operations.Handler
	Reconcile  func(context.Context, string) error
	Logger     *slog.Logger
}

func (w *Worker) Process(ctx context.Context) error {
	raw, e := w.Q.ReadJobs(ctx)
	if e != nil {
		return e
	}
	var messages []message
	if e = json.Unmarshal(raw, &messages); e != nil {
		return e
	}
	for _, m := range messages {
		var p Payload
		e = json.Unmarshal(m.Payload, &p)
		if e == nil {
			switch p.Type {
			case "expire_request":
				e = w.Operations.Expire(ctx, p.BusinessID, p.RecordID, p.Module)
			case "reconcile_subscription":
				if w.Reconcile == nil {
					e = fmt.Errorf("billing reconciler unavailable")
				} else {
					e = w.Reconcile(ctx, p.CustomerID)
				}
			default:
				e = fmt.Errorf("unknown job type")
			}
		}
		if e == nil {
			if _, e = w.Q.AckJob(ctx, m.ID); e != nil {
				return e
			}
			continue
		}
		// Do not log payloads or customer data. Failed jobs remain inspectable/replayable.
		if w.Logger != nil {
			w.Logger.Warn("job failed", "message_id", m.ID, "attempt", m.ReadCount, "type", p.Type)
		}
		if m.ReadCount >= 8 {
			e = w.Q.FailJob(ctx, db.FailJobParams{MessageID: m.ID, Payload: m.Payload})
		} else {
			delay := min(3600, 5*(1<<min(m.ReadCount, 10)))
			e = w.Q.RetryJob(ctx, db.RetryJobParams{MessageID: m.ID, Seconds: int32(delay)})
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (w *Worker) Sweep(ctx context.Context) error {
	a, e := w.Q.DueAppointments(ctx)
	if e != nil {
		return e
	}
	for _, r := range a {
		if e = w.Operations.Expire(ctx, r.BusinessID, r.ID, "appointments"); e != nil {
			return e
		}
	}
	p, e := w.Q.DuePreorders(ctx)
	if e != nil {
		return e
	}
	for _, r := range p {
		if e = w.Operations.Expire(ctx, r.BusinessID, r.ID, "preorders"); e != nil {
			return e
		}
	}
	if e = w.Q.CleanRequestReplay(ctx); e != nil {
		return e
	}
	if e = w.Q.CleanRequestAccess(ctx); e != nil {
		return e
	}
	return w.Q.CleanJobArchive(ctx)
}
func (w *Worker) ReconcileAll(ctx context.Context) error {
	if w.Reconcile == nil {
		return nil
	}
	for offset := int32(0); ; offset += 100 {
		rows, e := w.Q.ListBillingCustomers(ctx, db.ListBillingCustomersParams{Limit: 100, Offset: offset})
		if e != nil {
			return e
		}
		for _, customer := range rows {
			if e = w.Reconcile(ctx, customer.String); e != nil {
				return e
			}
		}
		if len(rows) < 100 {
			return nil
		}
	}
}
func (w *Worker) Run(ctx context.Context) error {
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	sweep := time.NewTicker(time.Minute)
	defer sweep.Stop()
	reconcile := time.NewTicker(15 * time.Minute)
	defer reconcile.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
			jobCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			e := w.Process(jobCtx)
			cancel()
			if e != nil && w.Logger != nil {
				w.Logger.Error("queue poll failed", "error", e)
			}
		case <-sweep.C:
			if e := w.Sweep(ctx); e != nil && w.Logger != nil {
				w.Logger.Error("queue recovery sweep failed", "error", e)
			}
		case <-reconcile.C:
			if e := w.ReconcileAll(ctx); e != nil && w.Logger != nil {
				w.Logger.Warn("billing reconciliation sweep failed", "error", e)
			}
		}
	}
}
