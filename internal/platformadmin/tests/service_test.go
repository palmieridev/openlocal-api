package platformadmin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

type actorStore struct {
	subject string
	calls   int
	err     error
}

func (s *actorStore) ClerkUserID(context.Context, uuid.UUID) (string, error) {
	s.calls++
	return s.subject, s.err
}

// Nil inspection/access ports make any disclosure or work after rejection fail.
func TestInspectionServiceRequiresAllowlistAndMatchingLocalActor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actor   application.Actor
		allowed []string
		subject string
		err     error
	}{
		{name: "missing identity"},
		{name: "ordinary owner", actor: application.Actor{ClerkUserID: "user_owner", UserID: uuid.New()}, allowed: []string{"user_admin"}},
		{name: "missing allowlist", actor: application.Actor{ClerkUserID: "user_admin", UserID: uuid.New()}},
		{name: "forged local actor", actor: application.Actor{ClerkUserID: "user_admin", UserID: uuid.New()}, allowed: []string{"user_admin"}, subject: "user_owner"},
		{name: "deleted actor", actor: application.Actor{ClerkUserID: "user_admin", UserID: uuid.New()}, allowed: []string{"user_admin"}, err: application.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actors := &actorStore{subject: tc.subject, err: tc.err}
			svc := application.New(application.NewAuthorizer(actors, tc.allowed), nil, nil)
			_, err := svc.Search(t.Context(), tc.actor, application.SearchQuery{Limit: 25})
			assertForbidden(t, err)
			_, err = svc.Detail(t.Context(), tc.actor, uuid.New())
			assertForbidden(t, err)
			_, err = svc.Activity(t.Context(), tc.actor, application.ActivityQuery{Limit: 25})
			assertForbidden(t, err)
			if (tc.actor.ClerkUserID == "" || len(tc.allowed) == 0 || tc.actor.ClerkUserID == "user_owner") && actors.calls != 0 {
				t.Fatal("untrusted identity reached persistence")
			}
		})
	}
}
func assertForbidden(t *testing.T, err error) {
	t.Helper()
	var e *application.Error
	if !errors.As(err, &e) || e.Kind != application.Forbidden || e.Code != "platform_admin_required" {
		t.Fatalf("err=%v", err)
	}
}
func TestInspectionServiceValidatesBeforeReading(t *testing.T) {
	a := application.Actor{ClerkUserID: "user_admin", UserID: uuid.New()}
	svc := application.New(application.NewAuthorizer(&actorStore{subject: a.ClerkUserID}, []string{a.ClerkUserID}), nil, nil)
	for _, in := range []application.SearchQuery{{Limit: 0}, {Limit: 101}, {Limit: 25, Before: application.Position{At: time.Now()}}} {
		_, err := svc.Search(t.Context(), a, in)
		var e *application.Error
		if !errors.As(err, &e) || e.Kind != application.InvalidInput {
			t.Fatalf("err=%v", err)
		}
	}
}
func TestAuthorizerPropagatesIdentityStoreFailure(t *testing.T) {
	want := errors.New("database unavailable")
	a := application.Actor{ClerkUserID: "user_admin", UserID: uuid.New()}
	auth := application.NewAuthorizer(&actorStore{err: want}, []string{a.ClerkUserID})
	if err := auth.Check(t.Context(), a); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}
