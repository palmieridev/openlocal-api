package auth

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
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
	user, err := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{
		ClerkUserID: a.ClerkUserID,
		Email:       nullString(a.Email),
		FirstName:   nullString(a.FirstName),
		LastName:    nullString(a.LastName),
		ImageUrl:    nullString(a.ImageURL),
	})
	if err != nil {
		return a, db.User{}, err
	}
	a.UserID = user.ID
	return a, user, nil
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func MapClerkRole(role string) string {
	switch role {
	case "org:admin", "admin", "owner":
		return "owner"
	case "manager":
		return "manager"
	case "org:member", "member", "staff":
		return "staff"
	default:
		return ""
	}
}
