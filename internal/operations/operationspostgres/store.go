package operationspostgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/entitlements/accesspostgres"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Store struct {
	*accesspostgres.Store
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool, q *db.Queries) *Store { return &Store{accesspostgres.New(q), pool, q} }

var _ application.Repository = (*Store)(nil)
var _ application.Transactions = (*Store)(nil)
var _ application.Transaction = (*transaction)(nil)

type transaction struct {
	*Store
	tx pgx.Tx
}

func (t *transaction) Commit(ctx context.Context) error   { return translate(t.tx.Commit(ctx)) }
func (t *transaction) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
func (s *Store) Begin(ctx context.Context, business uuid.UUID) (application.Transaction, error) {
	if err := s.q.EnsureSubscription(ctx, business); err != nil {
		return nil, translate(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	q := s.q.WithTx(tx)
	if _, err = q.LockSubscription(ctx, business); err != nil {
		_ = tx.Rollback(ctx)
		return nil, translate(err)
	}
	return &transaction{New(s.pool, q), tx}, nil
}
func (s *Store) EnsureSubscription(ctx context.Context, business uuid.UUID) error {
	return translate(s.q.EnsureSubscription(ctx, business))
}
func (s *Store) GetSubscription(ctx context.Context, business uuid.UUID) (access.Subscription, error) {
	r, err := s.q.GetSubscription(ctx, business)
	return accesspostgres.Subscription(r), translate(err)
}
func (s *Store) Settings(ctx context.Context, business uuid.UUID, module string) ([]byte, bool, error) {
	r, err := s.q.GetModule(ctx, db.GetModuleParams{BusinessID: business, Module: module})
	return r.Settings, r.Enabled, translate(err)
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	return err
}
func stamp(t time.Time) pgtype.Timestamptz   { return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()} }
func instant(t pgtype.Timestamptz) time.Time { return t.Time }
func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}
func stringPointer(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}
