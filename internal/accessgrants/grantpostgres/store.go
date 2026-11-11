// Package grantpostgres implements the business-scoped, serialized grant store.
package grantpostgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/palmieridev/openlocal-api/internal/audit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/application"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	admin "github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool, q *db.Queries) *Store { return &Store{pool, q} }
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.ErrNotFound
	}
	return err
}
func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()} }
func expiry(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return stamp(*t)
}
func grant(g db.BusinessAccessGrant) application.Grant {
	return application.Grant{ID: g.ID, BusinessID: g.BusinessID, Module: g.Module, Tier: g.Tier, StartsAt: g.StartsAt.Time, ExpiresAt: api.TSP(g.ExpiresAt), InternalReason: g.InternalReason, OwnerMessage: api.StringPtr(g.OwnerMessage), CreatedBy: api.UUIDPtr(g.CreatedBy), CreatedAt: g.CreatedAt.Time, UpdatedAt: g.UpdatedAt.Time, Version: g.Version, RevokedAt: api.TSP(g.RevokedAt), RevokedBy: api.UUIDPtr(g.RevokedBy), RevocationReason: api.StringPtr(g.RevocationReason)}
}
func (s *Store) RequireBusiness(ctx context.Context, id uuid.UUID) error {
	_, err := s.q.ModuleBusiness(ctx, id)
	return translate(err)
}
func (s *Store) Grant(ctx context.Context, key application.ScopedID) (application.Grant, error) {
	g, err := s.q.GetAccessGrant(ctx, db.GetAccessGrantParams{BusinessID: key.BusinessID, ID: key.ID})
	return grant(g), translate(err)
}
func (s *Store) Grants(ctx context.Context, business uuid.UUID, page application.Page) ([]application.Grant, error) {
	rows, err := s.q.ListAccessGrants(ctx, db.ListAccessGrantsParams{BusinessID: business, PageLimit: page.Limit, PageOffset: page.Offset})
	if err != nil {
		return nil, err
	}
	out := make([]application.Grant, 0, len(rows))
	for _, r := range rows {
		out = append(out, grant(r))
	}
	return out, nil
}
func (s *Store) Count(ctx context.Context, business uuid.UUID) (int32, error) {
	return s.q.CountAccessGrants(ctx, business)
}
func (s *Store) History(ctx context.Context, key application.ScopedID, page application.Page) ([]application.History, error) {
	rows, err := s.q.AccessGrantHistory(ctx, db.AccessGrantHistoryParams{BusinessID: uuid.NullUUID{UUID: key.BusinessID, Valid: true}, EntityID: uuid.NullUUID{UUID: key.ID, Valid: true}, PageLimit: page.Limit, PageOffset: page.Offset})
	if err != nil {
		return nil, err
	}
	out := make([]application.History, 0, len(rows))
	for _, r := range rows {
		out = append(out, application.History{ID: r.ID, ActorUserID: api.UUIDPtr(r.ActorUserID), Action: r.Action, At: r.CreatedAt.Time, Metadata: json.RawMessage(r.Metadata)})
	}
	return out, nil
}
func (s *Store) WithinBusiness(ctx context.Context, business uuid.UUID, fn func(application.Writer) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	store := New(s.pool, s.q.WithTx(tx))
	if err = store.RequireBusiness(ctx, business); err != nil {
		return err
	}
	if err = store.q.EnsureSubscription(ctx, business); err != nil {
		return err
	}
	if _, err = store.q.LockSubscription(ctx, business); err != nil {
		return translate(err)
	}
	if err = fn(store); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Create(ctx context.Context, in application.Creation) (application.Grant, error) {
	g, err := s.q.CreateAccessGrant(ctx, db.CreateAccessGrantParams{BusinessID: in.BusinessID, Module: in.Input.Module, Tier: in.Input.Tier, StartsAt: stamp(in.Input.StartsAt), ExpiresAt: expiry(in.Input.ExpiresAt), InternalReason: in.Input.InternalReason, OwnerMessage: api.NullString(in.Input.OwnerMessage), CreatedBy: uuid.NullUUID{UUID: in.ActorID, Valid: true}})
	return grant(g), translate(err)
}
func (s *Store) Extend(ctx context.Context, in application.Extension) (application.Grant, error) {
	g, err := s.q.ExtendAccessGrant(ctx, db.ExtendAccessGrantParams{BusinessID: in.Key.BusinessID, ID: in.Key.ID, ExpiresAt: expiry(in.ExpiresAt), Version: in.Version})
	return grant(g), translate(err)
}
func (s *Store) Revoke(ctx context.Context, in application.Revocation) (application.Grant, error) {
	g, err := s.q.RevokeAccessGrant(ctx, db.RevokeAccessGrantParams{BusinessID: in.Key.BusinessID, ID: in.Key.ID, RevokedBy: uuid.NullUUID{UUID: in.ActorID, Valid: true}, RevocationReason: api.NullString(&in.Reason), Version: in.Version})
	return grant(g), translate(err)
}
func (s *Store) Audit(ctx context.Context, in application.Change) error {
	return audit.WithMetadata(s.q, ctx, in.BusinessID, in.ActorID, "access_grant."+in.Action, "access_grant", in.GrantID, map[string]any{"internal_reason": in.Reason, "before": in.Before, "after": in.After})
}
