// Package identity synchronizes trusted, verified identity-provider facts.
// Callers must verify webhook signatures or session claims before invoking it.
package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidUser = errors.New("identity user ID is required")

type Profile struct {
	ClerkUserID                          string
	Email, FirstName, LastName, ImageURL string
}
type User struct {
	ID uuid.UUID
	Profile
	CreatedAt, UpdatedAt time.Time
}

type UserWriter interface {
	UpsertUser(context.Context, Profile) (User, error)
}
type Users struct{ writer UserWriter }

func NewUsers(writer UserWriter) *Users { return &Users{writer: writer} }

// Upsert accepts a verified identity, never a caller-selected local user ID.
func (s *Users) Upsert(ctx context.Context, profile Profile) (User, error) {
	if profile.ClerkUserID == "" {
		return User{}, ErrInvalidUser
	}
	return s.writer.UpsertUser(ctx, profile)
}

// Event contains provider facts already decoded and authenticated by the edge.
// Membership profiles must use public_user_data.user_id, not the event's ID.
type Event struct {
	Type                 string
	User                 Profile
	OrganizationID, Role string
}
type Result struct{ Applied bool }

type Store interface {
	UserWriter
	DeleteUser(context.Context, string) error
	ArchiveOrganization(context.Context, string) error
	DeleteMembership(context.Context, string, string) error
	BeginMembership(context.Context) (MembershipTransaction, error)
}

// MembershipTransaction resolves the business from the trusted organization and
// commits the profile and local grant together. Missing organizations return false.
type MembershipTransaction interface {
	UserWriter
	BusinessForOrganization(context.Context, string) (uuid.UUID, bool, error)
	PutMembership(context.Context, uuid.UUID, uuid.UUID, string, string) error
	Commit(context.Context) error
	Rollback(context.Context) error
}
type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

func (s *Service) Synchronize(ctx context.Context, event Event) (Result, error) {
	switch event.Type {
	case "user.created", "user.updated":
		if event.User.ClerkUserID == "" {
			return Result{}, nil
		}
		_, err := NewUsers(s.store).Upsert(ctx, event.User)
		return Result{Applied: err == nil}, err
	case "user.deleted":
		if event.User.ClerkUserID == "" {
			return Result{}, nil
		}
		err := s.store.DeleteUser(ctx, event.User.ClerkUserID)
		return Result{Applied: err == nil}, err
	case "organization.deleted":
		if event.OrganizationID == "" {
			return Result{}, nil
		}
		err := s.store.ArchiveOrganization(ctx, event.OrganizationID)
		return Result{Applied: err == nil}, err
	case "organizationMembership.created", "organizationMembership.updated":
		return s.membership(ctx, event)
	case "organizationMembership.deleted":
		if event.OrganizationID == "" || event.User.ClerkUserID == "" {
			return Result{}, nil
		}
		err := s.store.DeleteMembership(ctx, event.OrganizationID, event.User.ClerkUserID)
		return Result{Applied: err == nil}, err
	default:
		// Organization creation/updates and unknown event types grant no local access.
		return Result{}, nil
	}
}
func (s *Service) membership(ctx context.Context, event Event) (Result, error) {
	role := MapClerkRole(event.Role)
	if event.OrganizationID == "" || event.User.ClerkUserID == "" || role == "" {
		return Result{}, nil
	}
	tx, err := s.store.BeginMembership(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	businessID, found, err := tx.BusinessForOrganization(ctx, event.OrganizationID)
	if err != nil || !found {
		return Result{}, err
	}
	user, err := NewUsers(tx).Upsert(ctx, event.User)
	if err != nil {
		return Result{}, err
	}
	if err := tx.PutMembership(ctx, businessID, user.ID, event.OrganizationID, role); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{Applied: true}, nil
}

// MapClerkRole preserves the local role policy used by verified JWT claims.
// Unknown roles have no authority; webhook callers must not supply a fallback.
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
