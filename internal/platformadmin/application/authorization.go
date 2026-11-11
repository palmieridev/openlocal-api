package application

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("not found")

type Failure string

const (
	InvalidInput Failure = "invalid_input"
	Forbidden    Failure = "forbidden"
	Conflict     Failure = "conflict"
)

type Error struct {
	Kind          Failure
	Code, Message string
}

func (e *Error) Error() string     { return e.Message }
func Invalid(message string) error { return &Error{InvalidInput, "invalid_request", message} }

// Actor contains the verified Clerk subject and the locally resolved user ID.
// Only an authenticated transport may supply the Clerk subject; it must never
// come from a body, query parameter, business membership, or organization role.
// Services recheck the configured allowlist and the persisted subject/ID pair.
type Actor struct {
	ClerkUserID string
	UserID      uuid.UUID
}
type Actors interface {
	ClerkUserID(context.Context, uuid.UUID) (string, error)
}
type Authorizer struct {
	actors  Actors
	allowed []string
}

func NewAuthorizer(actors Actors, allowed []string) *Authorizer {
	return &Authorizer{actors: actors, allowed: slices.Clone(allowed)}
}
func (a *Authorizer) Check(ctx context.Context, actor Actor) error {
	denied := &Error{Forbidden, "platform_admin_required", "platform administrator access is required"}
	if a == nil || actor.ClerkUserID == "" || actor.UserID == uuid.Nil || !slices.Contains(a.allowed, actor.ClerkUserID) {
		return denied
	}
	subject, err := a.actors.ClerkUserID(ctx, actor.UserID)
	if errors.Is(err, ErrNotFound) {
		return denied
	}
	if err != nil {
		return err
	}
	if subject != actor.ClerkUserID {
		return denied
	}
	return nil
}
