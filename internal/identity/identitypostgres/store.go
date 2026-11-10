// Package identitypostgres translates identity ports to sqlc/pgx persistence.
package identitypostgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/identity"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Users struct{ q *db.Queries }

func NewUsers(q *db.Queries) *Users { return &Users{q: q} }
func (s *Users) UpsertUser(ctx context.Context, p identity.Profile) (identity.User, error) {
	row, err := s.q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: p.ClerkUserID, Email: nullable(p.Email), FirstName: nullable(p.FirstName), LastName: nullable(p.LastName), ImageUrl: nullable(p.ImageURL)})
	if err != nil {
		return identity.User{}, err
	}
	return identity.User{ID: row.ID, Profile: identity.Profile{ClerkUserID: row.ClerkUserID, Email: row.Email.String, FirstName: row.FirstName.String, LastName: row.LastName.String, ImageURL: row.ImageUrl.String}, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}, nil
}

// UserRow keeps legacy callers' sqlc return contract at the adapter boundary.
func UserRow(u identity.User) db.User {
	return db.User{ID: u.ID, ClerkUserID: u.ClerkUserID, Email: nullable(u.Email), FirstName: nullable(u.FirstName), LastName: nullable(u.LastName), ImageUrl: nullable(u.ImageURL), CreatedAt: pgtype.Timestamptz{Time: u.CreatedAt, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: u.UpdatedAt, Valid: true}}
}
func nullable(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

type Store struct {
	*Users
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool, q *db.Queries) *Store { return &Store{Users: NewUsers(q), pool: pool} }
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	return s.q.DeleteUserByClerkID(ctx, id)
}
func (s *Store) ArchiveOrganization(ctx context.Context, org string) error {
	return s.q.ArchiveBusinessesByClerkOrgID(ctx, org)
}
func (s *Store) DeleteMembership(ctx context.Context, org, user string) error {
	u, err := s.q.GetUserByClerkID(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.q.DeleteBusinessMemberByClerkOrgAndUser(ctx, db.DeleteBusinessMemberByClerkOrgAndUserParams{ClerkOrgID: org, UserID: u.ID})
}
func (s *Store) BeginMembership(ctx context.Context) (identity.MembershipTransaction, error) {
	if s.pool == nil {
		return nil, errors.New("identity transaction database is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &membershipTx{Users: NewUsers(s.q.WithTx(tx)), tx: tx}, nil
}

type membershipTx struct {
	*Users
	tx pgx.Tx
}

func (t *membershipTx) BusinessForOrganization(ctx context.Context, org string) (uuid.UUID, bool, error) {
	id, err := t.q.GetBusinessIDByClerkOrgID(ctx, org)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}
func (t *membershipTx) PutMembership(ctx context.Context, business, user uuid.UUID, org, role string) error {
	// Recheck the organization/business relationship at the persistence boundary.
	id, found, err := t.BusinessForOrganization(ctx, org)
	if err != nil {
		return err
	}
	if !found || id != business {
		return errors.New("identity organization does not match business")
	}
	_, err = t.q.AddBusinessMember(ctx, db.AddBusinessMemberParams{BusinessID: business, UserID: user, ClerkOrgID: org, Role: role})
	return err
}
func (t *membershipTx) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *membershipTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

var _ identity.Store = (*Store)(nil)
var _ identity.MembershipTransaction = (*membershipTx)(nil)
