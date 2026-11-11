package savedpostgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/savedbusinesses/personal"
)

type Store struct{ q *db.Queries }

func New(q *db.Queries) *Store { return &Store{q} }
func (s *Store) State(ctx context.Context, user, business uuid.UUID) (bool, error) {
	return s.q.IsBusinessSaved(ctx, db.IsBusinessSavedParams{UserID: user, BusinessID: business})
}
func (s *Store) Save(ctx context.Context, user, business uuid.UUID) error {
	_, err := s.q.SaveBusiness(ctx, db.SaveBusinessParams{UserID: user, BusinessID: business})
	if errors.Is(err, pgx.ErrNoRows) {
		return personal.ErrNotFound
	}
	return err
}
func (s *Store) Remove(ctx context.Context, user, business uuid.UUID) error {
	return s.q.RemoveSavedBusiness(ctx, db.RemoveSavedBusinessParams{UserID: user, BusinessID: business})
}
func (s *Store) List(ctx context.Context, user uuid.UUID, page personal.Page) ([]personal.Record, error) {
	rows, err := s.q.ListSavedBusinesses(ctx, db.ListSavedBusinessesParams{UserID: user, BeforeAt: pgtype.Timestamptz{Time: page.BeforeAt, Valid: !page.BeforeAt.IsZero()}, BeforeID: page.BeforeID, PageLimit: page.Limit})
	if err != nil {
		return nil, err
	}
	out := make([]personal.Record, 0, len(rows))
	for _, r := range rows {
		out = append(out, personal.Record{BusinessID: r.BusinessID, SavedAt: r.CreatedAt.Time, Status: r.Status, Name: r.Name, Slug: r.Slug, LogoURL: api.StringPtr(r.LogoUrl)})
	}
	return out, nil
}
