package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/identity"
)

type fakeStore struct {
	tx             *fakeMembership
	begins, writes int
}

func (s *fakeStore) UpsertUser(context.Context, identity.Profile) (identity.User, error) {
	s.writes++
	return identity.User{}, nil
}
func (s *fakeStore) DeleteUser(context.Context, string) error               { s.writes++; return nil }
func (s *fakeStore) ArchiveOrganization(context.Context, string) error      { s.writes++; return nil }
func (s *fakeStore) DeleteMembership(context.Context, string, string) error { s.writes++; return nil }
func (s *fakeStore) BeginMembership(context.Context) (identity.MembershipTransaction, error) {
	s.begins++
	return s.tx, nil
}

type fakeMembership struct {
	found                 bool
	org                   string
	user                  identity.Profile
	role                  string
	business, member      uuid.UUID
	fail                  error
	committed, rolledBack bool
}

func (t *fakeMembership) BusinessForOrganization(_ context.Context, org string) (uuid.UUID, bool, error) {
	t.org = org
	return t.business, t.found, nil
}
func (t *fakeMembership) UpsertUser(_ context.Context, p identity.Profile) (identity.User, error) {
	t.user = p
	return identity.User{ID: t.member, Profile: p}, nil
}
func (t *fakeMembership) PutMembership(_ context.Context, business, user uuid.UUID, org, role string) error {
	if business != t.business || user != t.member || org != t.org {
		panic("membership scope lost")
	}
	t.role = role
	return t.fail
}
func (t *fakeMembership) Commit(context.Context) error   { t.committed = true; return nil }
func (t *fakeMembership) Rollback(context.Context) error { t.rolledBack = true; return nil }
func TestIdentityIgnoresUnrecognizedOrIncompleteEvents(t *testing.T) {
	for _, event := range []identity.Event{
		{Type: "unknown", User: identity.Profile{ClerkUserID: "user"}, OrganizationID: "org", Role: "org:admin"},
		{Type: "organization.created", OrganizationID: "org"}, {Type: "organization.updated", OrganizationID: "org"},
		{Type: "user.created"}, {Type: "user.deleted"}, {Type: "organization.deleted"},
		{Type: "organizationMembership.created", OrganizationID: "org", Role: "org:admin"},
		{Type: "organizationMembership.updated", User: identity.Profile{ClerkUserID: "user"}, Role: "org:admin"},
		{Type: "organizationMembership.created", User: identity.Profile{ClerkUserID: "user"}, OrganizationID: "org", Role: "unknown"},
		{Type: "organizationMembership.deleted", OrganizationID: "org"},
	} {
		s := &fakeStore{}
		result, err := identity.New(s).Synchronize(context.Background(), event)
		if err != nil || result.Applied || s.writes != 0 || s.begins != 0 {
			t.Fatalf("event %+v produced writes/result=%+v err=%v", event, result, err)
		}
	}
}
func TestIdentityMembershipDecisions(t *testing.T) {
	injected := errors.New("membership failed")
	for _, tc := range []struct {
		name  string
		found bool
		err   error
	}{{"unknown org", false, nil}, {"member", true, nil}, {"failure", true, injected}} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &fakeMembership{found: tc.found, fail: tc.err, business: uuid.New(), member: uuid.New()}
			s := &fakeStore{tx: tx}
			result, err := identity.New(s).Synchronize(context.Background(), identity.Event{Type: "organizationMembership.updated", OrganizationID: "org_verified", User: identity.Profile{ClerkUserID: "user_verified", FirstName: "Member"}, Role: "org:member"})
			if !errors.Is(err, tc.err) {
				t.Fatalf("error=%v want=%v", err, tc.err)
			}
			if !tc.found {
				if tx.user.ClerkUserID != "" || result.Applied {
					t.Fatal("unknown org persisted a user")
				}
			} else if tc.err == nil {
				if !result.Applied || !tx.committed || tx.user.ClerkUserID != "user_verified" || tx.role != "staff" {
					t.Fatal("verified membership not applied")
				}
			} else if tx.committed || result.Applied || !tx.rolledBack {
				t.Fatal("failed membership committed")
			}
		})
	}
}
func TestIdentityUpsertRequiresVerifiedUserID(t *testing.T) {
	s := &fakeStore{}
	_, err := identity.NewUsers(s).Upsert(context.Background(), identity.Profile{})
	if !errors.Is(err, identity.ErrInvalidUser) || s.writes != 0 {
		t.Fatal("empty user ID persisted")
	}
}
