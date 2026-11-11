// Package adminpostgres translates sqlc rows into private inspection snapshots.
package adminpostgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/entitlements/accesspostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

type Store struct{ q *db.Queries }

func New(q *db.Queries) *Store { return &Store{q} }
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	return err
}
func (s *Store) ClerkUserID(ctx context.Context, id uuid.UUID) (string, error) {
	u, err := s.q.GetUserByID(ctx, id)
	return u.ClerkUserID, translate(err)
}
func (s *Store) SearchBusinesses(ctx context.Context, in application.SearchQuery) ([]application.Business, error) {
	rows, err := s.q.AdminSearchBusinesses(ctx, db.AdminSearchBusinessesParams{SearchQuery: in.Query, BeforeAt: pgtype.Timestamptz{Time: in.Before.At, Valid: !in.Before.At.IsZero()}, BeforeID: in.Before.ID, LimitCount: in.Limit})
	if err != nil {
		return nil, err
	}
	out := make([]application.Business, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapBusiness(db.AdminGetBusinessRow(r)))
	}
	return out, nil
}
func (s *Store) Business(ctx context.Context, id uuid.UUID) (application.Business, error) {
	r, err := s.q.AdminGetBusiness(ctx, id)
	return mapBusiness(r), translate(err)
}
func (s *Store) Subscription(ctx context.Context, id uuid.UUID) (application.SubscriptionSnapshot, error) {
	r, err := s.q.GetSubscription(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.SubscriptionSnapshot{}, nil
	}
	if err != nil {
		return application.SubscriptionSnapshot{}, err
	}
	return application.SubscriptionSnapshot{Policy: accesspostgres.Subscription(r), Detail: mapSubscription(r)}, nil
}
func optionalID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}
func (s *Store) Activity(ctx context.Context, in application.ActivityQuery) ([]application.Activity, error) {
	rows, err := s.q.AdminListActivity(ctx, db.AdminListActivityParams{BusinessID: optionalID(in.BusinessID), ActorUserID: optionalID(in.ActorUserID), ActionFilter: in.Action, BeforeAt: pgtype.Timestamptz{Time: in.Before.At, Valid: !in.Before.At.IsZero()}, BeforeID: in.Before.ID, LimitCount: in.Limit})
	if err != nil {
		return nil, err
	}
	out := make([]application.Activity, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapActivity(r))
	}
	return out, nil
}
