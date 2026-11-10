// Package billingpostgres implements billing ports with sqlc and pgx.
package billingpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/entitlements/accesspostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool, q *db.Queries) *Store { return &Store{pool: pool, q: q} }

var _ application.Store = (*Store)(nil)
var _ application.Transaction = (*transaction)(nil)

func subscription(s db.BusinessSubscription) application.Subscription {
	return application.Subscription{Subscription: accesspostgres.Subscription(s), CustomerID: s.StripeCustomerID.String, SubscriptionID: s.StripeSubscriptionID.String, CancelAtPeriodEnd: s.CancelAtPeriodEnd, ScheduledChange: s.ScheduledChange}
}
func stamp(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}
func text(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func params(s application.Subscription) db.UpdateSubscriptionParams {
	return db.UpdateSubscriptionParams{BusinessID: s.BusinessID, Tier: s.Tier, Status: s.Status, Modules: s.Modules, StripeCustomerID: text(s.CustomerID), StripeSubscriptionID: text(s.SubscriptionID), PeriodStart: stamp(s.PeriodStart), PeriodEnd: stamp(s.PeriodEnd), GraceUntil: stamp(s.GraceUntil), CancelAtPeriodEnd: s.CancelAtPeriodEnd, ScheduledChange: s.ScheduledChange}
}
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	return err
}
func (s *Store) EnsureSubscription(ctx context.Context, business uuid.UUID) error {
	return s.q.EnsureSubscription(ctx, business)
}
func (s *Store) Subscription(ctx context.Context, business uuid.UUID) (application.Subscription, error) {
	row, err := s.q.GetSubscription(ctx, business)
	return subscription(row), notFound(err)
}
func (s *Store) ByCustomer(ctx context.Context, customer string) (application.Subscription, error) {
	row, err := s.q.GetSubscriptionByCustomer(ctx, text(customer))
	return subscription(row), notFound(err)
}
func (s *Store) Access(ctx context.Context, sub application.Subscription, role string, now time.Time) (access.Result, error) {
	return access.New(accesspostgres.New(s.q)).Get(ctx, sub.Subscription, role, now)
}
func (s *Store) ProcessEvents(ctx context.Context, customer string) error {
	return s.q.ProcessBillingEvents(ctx, customer)
}
func (s *Store) Begin(ctx context.Context) (application.Transaction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &transaction{tx: tx, q: s.q.WithTx(tx)}, nil
}

type transaction struct {
	tx pgx.Tx
	q  *db.Queries
}

func (t *transaction) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *transaction) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
func (t *transaction) LockSubscription(ctx context.Context, business uuid.UUID) (application.Subscription, error) {
	row, err := t.q.LockSubscription(ctx, business)
	return subscription(row), notFound(err)
}
func (t *transaction) UpdateSubscription(ctx context.Context, sub application.Subscription) error {
	_, err := t.q.UpdateSubscription(ctx, params(sub))
	return err
}
func (t *transaction) Operation(ctx context.Context, business uuid.UUID, key string) (application.Operation, error) {
	o, err := t.q.GetBillingOperation(ctx, db.GetBillingOperationParams{BusinessID: business, Key: key})
	return application.Operation{Digest: o.Digest, Response: o.Response}, notFound(err)
}
func (t *transaction) SaveOperation(ctx context.Context, business uuid.UUID, key string, o application.Operation) error {
	return t.q.SaveBillingOperation(ctx, db.SaveBillingOperationParams{BusinessID: business, Key: key, Digest: o.Digest, Response: o.Response})
}
func (t *transaction) Resources(ctx context.Context, business uuid.UUID) ([]application.Resource, error) {
	rows, err := t.q.ListAppointmentResources(ctx, business)
	if err != nil {
		return nil, err
	}
	resources := make([]application.Resource, len(rows))
	for i, r := range rows {
		resources[i] = application.Resource{ID: r.ID, Active: r.Active}
	}
	return resources, nil
}
func (t *transaction) BusinessAppointmentGrant(ctx context.Context, business uuid.UUID, now time.Time) (bool, error) {
	c, err := access.New(accesspostgres.New(t.q)).Capability(ctx, access.Subscription{BusinessID: business, Tier: "free"}, "appointments", now)
	return c.Tier == "business", err
}
func (t *transaction) DeactivateExtras(ctx context.Context, business, retained uuid.UUID) error {
	return t.q.DeactivateExtraAppointmentResources(ctx, db.DeactivateExtraAppointmentResourcesParams{BusinessID: business, ID: retained})
}
func (t *transaction) ProcessEvents(ctx context.Context, customer string) error {
	return t.q.ProcessBillingEvents(ctx, customer)
}
func (t *transaction) SaveEventAndEnqueue(ctx context.Context, id, customer string) error {
	inserted, err := t.q.SaveBillingEvent(ctx, db.SaveBillingEventParams{ID: id, CustomerID: customer})
	if err != nil || inserted != 1 {
		return err
	}
	payload, err := json.Marshal(map[string]string{"type": "reconcile_subscription", "customer_id": customer})
	if err != nil {
		return err
	}
	_, err = t.q.EnqueueJob(ctx, db.EnqueueJobParams{Payload: payload, DelaySeconds: 0})
	return err
}
