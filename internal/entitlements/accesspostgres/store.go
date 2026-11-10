// Package accesspostgres binds access decisions to sqlc, including caller-owned transactions.
package accesspostgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Store struct{ q *db.Queries }

func New(q *db.Queries) *Store { return &Store{q: q} }

var _ access.Store = (*Store)(nil)

func instant(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
func Subscription(s db.BusinessSubscription) access.Subscription {
	return access.Subscription{BusinessID: s.BusinessID, Tier: s.Tier, Status: s.Status, Modules: s.Modules, PeriodStart: instant(s.PeriodStart), PeriodEnd: instant(s.PeriodEnd), GraceUntil: instant(s.GraceUntil)}
}
func Grant(g db.BusinessAccessGrant) access.Grant {
	var message *string
	if g.OwnerMessage.Valid {
		message = &g.OwnerMessage.String
	}
	return access.Grant{ID: g.ID, BusinessID: g.BusinessID, Module: g.Module, Tier: g.Tier, StartsAt: g.StartsAt.Time, ExpiresAt: instant(g.ExpiresAt), RevokedAt: instant(g.RevokedAt), OwnerMessage: message}
}
func (s *Store) Grants(ctx context.Context, business uuid.UUID, now time.Time) ([]access.Grant, error) {
	rows, err := s.q.AccessGrantsForResolution(ctx, db.AccessGrantsForResolutionParams{BusinessID: business, At: stamp(now)})
	if err != nil {
		return nil, err
	}
	grants := make([]access.Grant, len(rows))
	for i, g := range rows {
		grants[i] = Grant(g)
	}
	return grants, nil
}
func (s *Store) Enabled(ctx context.Context, business uuid.UUID, module string) (bool, error) {
	m, err := s.q.GetModule(ctx, db.GetModuleParams{BusinessID: business, Module: module})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return m.Enabled, err
}
func (s *Store) Usage(ctx context.Context, sub access.Subscription, module string, now time.Time) (int32, error) {
	if access.Paid(sub, now) {
		return s.q.ModuleUsage(ctx, db.ModuleUsageParams{BusinessID: sub.BusinessID, Module: module, PeriodStart: stamp(*sub.PeriodStart)})
	}
	start, end := access.Window(sub, now)
	return s.q.ModuleUsageInWindow(ctx, db.ModuleUsageInWindowParams{BusinessID: sub.BusinessID, Module: module, WindowStart: stamp(start), WindowEnd: stamp(end)})
}
func (s *Store) Consume(ctx context.Context, business uuid.UUID, module string, record uuid.UUID, start time.Time) error {
	return s.q.ConsumeModuleUsage(ctx, db.ConsumeModuleUsageParams{BusinessID: business, Module: module, RecordID: record, PeriodStart: stamp(start)})
}
