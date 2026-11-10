package auth

import (
	"context"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/identity"
	"github.com/palmieridev/openlocal-api/internal/identity/identitypostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Context struct {
	ClerkUserID string
	ClerkOrgID  string
	OrgRole     string
	Role        string
	Email       string
	FirstName   string
	LastName    string
	ImageURL    string
	UserID      uuid.UUID
}

type contextKey struct{}

func WithContext(ctx context.Context, auth Context) context.Context {
	return context.WithValue(ctx, contextKey{}, auth)
}

func FromContext(ctx context.Context) (Context, bool) {
	value, ok := ctx.Value(contextKey{}).(Context)
	return value, ok
}

func WithFiberLocals(base context.Context, auth Context) context.Context {
	return WithContext(base, auth)
}

func (a Context) UpsertUser(ctx context.Context, q *db.Queries) (Context, db.User, error) {
	user, err := identity.NewUsers(identitypostgres.NewUsers(q)).Upsert(ctx, identity.Profile{
		ClerkUserID: a.ClerkUserID, Email: a.Email, FirstName: a.FirstName, LastName: a.LastName, ImageURL: a.ImageURL,
	})
	if err != nil {
		return a, db.User{}, err
	}
	a.UserID = user.ID
	return a, identitypostgres.UserRow(user), nil
}

// MapClerkRole retains the existing auth policy entry point.
func MapClerkRole(role string) string { return identity.MapClerkRole(role) }
