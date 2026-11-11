package businesspostgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool, q *db.Queries) *Store { return &Store{pool, q} }
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return profile.ErrNotFound
	}
	return err
}
func (s *Store) WithinTransaction(ctx context.Context, fn func(profile.Transaction) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = fn(&transaction{s.q.WithTx(tx)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func business(ctx context.Context, q *db.Queries, b db.Business, err error) (profile.Business, error) {
	if err != nil {
		return profile.Business{}, translate(err)
	}
	areas, err := q.ListBusinessServiceAreas(ctx, b.ID)
	if err != nil {
		return profile.Business{}, err
	}
	return mapBusiness(b, areas), nil
}
func (s *Store) Business(ctx context.Context, id profile.Identity) (profile.Business, error) {
	b, err := s.q.GetBusinessForMember(ctx, db.GetBusinessForMemberParams{ID: id.BusinessID, UserID: id.ActorID})
	return business(ctx, s.q, b, err)
}
func (s *Store) ActiveBusiness(ctx context.Context, actor uuid.UUID, org string) (profile.Business, error) {
	id, err := s.q.GetBusinessIDByClerkOrgID(ctx, org)
	if err != nil {
		return profile.Business{}, translate(err)
	}
	role, err := s.q.GetBusinessMemberRole(ctx, db.GetBusinessMemberRoleParams{BusinessID: id, UserID: actor, ClerkOrgID: org})
	if errors.Is(err, pgx.ErrNoRows) {
		return profile.Business{}, profile.ErrMembership
	}
	if err != nil {
		return profile.Business{}, err
	}
	if role != "owner" && role != "manager" && role != "staff" {
		return profile.Business{}, profile.ErrRole
	}
	return s.Business(ctx, profile.Identity{BusinessID: id, ActorID: actor})
}
func (s *Store) Hours(ctx context.Context, id uuid.UUID) ([]profile.Hour, error) {
	rows, err := s.q.ListBusinessHours(ctx, id)
	return mapHours(rows), err
}

type transaction struct{ q *db.Queries }

func (t *transaction) LockBusiness(ctx context.Context, id profile.Identity) (profile.Business, error) {
	b, err := t.q.LockBusinessForManager(ctx, db.LockBusinessForManagerParams{ID: id.BusinessID, UserID: id.ActorID})
	return business(ctx, t.q, b, err)
}
func (t *transaction) replaceAreas(ctx context.Context, id uuid.UUID, areas []profile.Area) ([]db.BusinessServiceArea, error) {
	if err := t.q.DeleteBusinessServiceAreas(ctx, id); err != nil {
		return nil, err
	}
	rows := make([]db.BusinessServiceArea, 0, len(areas))
	for _, a := range areas {
		r, err := t.q.CreateBusinessServiceArea(ctx, areaParams(id, a))
		if err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}
func (t *transaction) Create(ctx context.Context, actor profile.Actor, org string, data profile.Data) (profile.Business, error) {
	params := createParams(data)
	params.ClerkOrgID = api.NullString(&org)
	b, err := t.q.CreateBusiness(ctx, params)
	if err != nil {
		return profile.Business{}, err
	}
	areas, err := t.replaceAreas(ctx, b.ID, data.Areas)
	if err != nil {
		return profile.Business{}, err
	}
	if _, err = t.q.AddBusinessMember(ctx, db.AddBusinessMemberParams{BusinessID: b.ID, UserID: actor.ID, ClerkOrgID: org, Role: "owner"}); err != nil {
		return profile.Business{}, err
	}
	if _, err = t.q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: b.ID, Name: "Default", IsDefault: true}); err != nil {
		return profile.Business{}, err
	}
	if err = api.Audit(t.q, ctx, b.ID, actor.ID, "business.create", "business", b.ID); err != nil {
		return profile.Business{}, err
	}
	return mapBusiness(b, areas), nil
}
func (t *transaction) Update(ctx context.Context, id profile.Identity, data profile.Data, replace bool) (profile.Business, error) {
	b, err := t.q.UpdateBusiness(ctx, updateParams(id, data))
	if err != nil {
		return profile.Business{}, translate(err)
	}
	var areas []db.BusinessServiceArea
	if replace {
		areas, err = t.replaceAreas(ctx, b.ID, data.Areas)
	} else {
		areas, err = t.q.ListBusinessServiceAreas(ctx, b.ID)
	}
	if err != nil {
		return profile.Business{}, err
	}
	if err = api.Audit(t.q, ctx, b.ID, id.ActorID, "business.update", "business", b.ID); err != nil {
		return profile.Business{}, err
	}
	return mapBusiness(b, areas), nil
}
func (t *transaction) ReplaceHours(ctx context.Context, id profile.Identity, hours []profile.Hour) ([]profile.Hour, error) {
	// Also guard the persistence operation if invoked independently of the service.
	if _, err := t.q.LockBusinessForManager(ctx, db.LockBusinessForManagerParams{ID: id.BusinessID, UserID: id.ActorID}); err != nil {
		return nil, translate(err)
	}
	if err := t.q.DeleteBusinessHours(ctx, id.BusinessID); err != nil {
		return nil, err
	}
	for _, h := range hours {
		if _, err := t.q.UpsertBusinessHour(ctx, db.UpsertBusinessHourParams{BusinessID: id.BusinessID, DayOfWeek: h.DayOfWeek, OpensAt: clock(h.OpensAt), ClosesAt: clock(h.ClosesAt), IsClosed: h.IsClosed}); err != nil {
			return nil, err
		}
	}
	if err := api.Audit(t.q, ctx, id.BusinessID, id.ActorID, "business.hours.update", "business", id.BusinessID); err != nil {
		return nil, err
	}
	rows, err := t.q.ListBusinessHours(ctx, id.BusinessID)
	return mapHours(rows), err
}
