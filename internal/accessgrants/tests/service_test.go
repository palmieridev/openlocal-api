package accessgrants_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/application"
	admin "github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

type actors struct{ subject string }

func (a actors) ClerkUserID(context.Context, uuid.UUID) (string, error) { return a.subject, nil }
func grantAuth(a admin.Actor) *admin.Authorizer {
	return admin.NewAuthorizer(actors{a.ClerkUserID}, []string{a.ClerkUserID})
}
func requireGrantFailure(t *testing.T, err error, kind admin.Failure) {
	t.Helper()
	var e *admin.Error
	if !errors.As(err, &e) || e.Kind != kind {
		t.Fatalf("err=%v want=%s", err, kind)
	}
}

func TestGrantServiceRejectsUntrustedActorsBeforeAnyResourceAccess(t *testing.T) {
	for _, a := range []admin.Actor{{}, {ClerkUserID: "user_owner", UserID: uuid.New()}, {ClerkUserID: "user_admin"}, {ClerkUserID: "user_admin", UserID: uuid.New()}} {
		// The last identity claims an allowlisted subject with a foreign local user.
		svc := application.New(admin.NewAuthorizer(actors{"user_owner"}, []string{"user_admin"}), nil, nil)
		b := uuid.New()
		key := application.ScopedID{BusinessID: b, ID: uuid.New()}
		_, err := svc.List(t.Context(), a, b, application.Page{Limit: 25})
		requireGrantFailure(t, err, admin.Forbidden)
		_, err = svc.Create(t.Context(), a, b, application.CreateInput{})
		requireGrantFailure(t, err, admin.Forbidden)
		_, err = svc.Extend(t.Context(), a, key, application.ExtendInput{})
		requireGrantFailure(t, err, admin.Forbidden)
		_, err = svc.Revoke(t.Context(), a, key, application.RevokeInput{})
		requireGrantFailure(t, err, admin.Forbidden)
		_, err = svc.History(t.Context(), a, key, application.Page{Limit: 25})
		requireGrantFailure(t, err, admin.Forbidden)
	}
}

type businessReader struct{ application.Reader }

func (businessReader) RequireBusiness(context.Context, uuid.UUID) error { return nil }
func TestGrantServiceValidatesCreationBeforeStartingTransaction(t *testing.T) {
	actor := admin.Actor{ClerkUserID: "user_admin", UserID: uuid.New()}
	svc := application.New(grantAuth(actor), businessReader{}, nil)
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	valid := application.CreateInput{Module: "appointments", Tier: "essential", StartsAt: now, Indefinite: true, InternalReason: "gift"}
	for _, tc := range []struct {
		name  string
		alter func(*application.CreateInput)
	}{
		{"module", func(in *application.CreateInput) { in.Module = "stock" }},
		{"tier", func(in *application.CreateInput) { in.Tier = "free" }},
		{"missing start", func(in *application.CreateInput) { in.StartsAt = time.Time{} }},
		{"invalid year", func(in *application.CreateInput) { in.StartsAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"missing reason", func(in *application.CreateInput) { in.InternalReason = " " }},
		{"long reason", func(in *application.CreateInput) { in.InternalReason = strings.Repeat("á", 2001) }},
		{"missing expiry choice", func(in *application.CreateInput) { in.Indefinite = false }},
		{"conflicting expiry", func(in *application.CreateInput) { in.ExpiresAt = &future }},
		{"expired", func(in *application.CreateInput) {
			in.StartsAt = past.Add(-time.Hour)
			in.ExpiresAt = &past
			in.Indefinite = false
		}},
		{"expiry before start", func(in *application.CreateInput) { in.ExpiresAt = &past; in.Indefinite = false }},
		{"empty owner message", func(in *application.CreateInput) { in.OwnerMessage = new(string) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.alter(&in)
			_, err := svc.Create(t.Context(), actor, uuid.New(), in)
			requireGrantFailure(t, err, admin.InvalidInput)
		})
	}
	for _, in := range []application.Page{{Limit: 0}, {Limit: 101}, {Limit: 25, Offset: -1}} {
		_, err := svc.List(t.Context(), actor, uuid.New(), in)
		requireGrantFailure(t, err, admin.InvalidInput)
	}
}

type grantReader struct {
	application.Reader
	grant application.Grant
}

func (r grantReader) RequireBusiness(context.Context, uuid.UUID) error { return nil }
func (r grantReader) Grant(context.Context, application.ScopedID) (application.Grant, error) {
	return r.grant, nil
}

func (r grantReader) History(context.Context, application.ScopedID, application.Page) ([]application.History, error) {
	return []application.History{{Action: "private"}}, nil
}

type grantWriter struct {
	application.Writer
	grant application.Grant
}

func (w grantWriter) Grant(context.Context, application.ScopedID) (application.Grant, error) {
	return w.grant, nil
}

type transaction struct{ writer application.Writer }

func (tx transaction) WithinBusiness(ctx context.Context, _ uuid.UUID, fn func(application.Writer) error) error {
	return fn(tx.writer)
}
func TestGrantServiceChecksReturnedResourceAssociation(t *testing.T) {
	actor := admin.Actor{ClerkUserID: "user_admin", UserID: uuid.New()}
	key := application.ScopedID{BusinessID: uuid.New(), ID: uuid.New()}
	for _, g := range []application.Grant{{ID: key.ID, BusinessID: uuid.New(), Version: 1}, {ID: uuid.New(), BusinessID: key.BusinessID, Version: 1}} {
		svc := application.New(grantAuth(actor), grantReader{grant: g}, transaction{grantWriter{grant: g}})
		_, err := svc.History(t.Context(), actor, key, application.Page{Limit: 25})
		if !errors.Is(err, admin.ErrNotFound) {
			t.Fatalf("history err=%v", err)
		}
		_, err = svc.Extend(t.Context(), actor, key, application.ExtendInput{Version: 1, Indefinite: true, InternalReason: "gift"})
		if !errors.Is(err, admin.ErrNotFound) {
			t.Fatalf("extend err=%v", err)
		}
		_, err = svc.Revoke(t.Context(), actor, key, application.RevokeInput{Version: 1, InternalReason: "end"})
		if !errors.Is(err, admin.ErrNotFound) {
			t.Fatalf("revoke err=%v", err)
		}
	}
}
func TestGrantServiceRejectsInvalidAndConflictingEdits(t *testing.T) {
	actor := admin.Actor{ClerkUserID: "user_admin", UserID: uuid.New()}
	key := application.ScopedID{BusinessID: uuid.New(), ID: uuid.New()}
	now := time.Now()
	end := now.Add(time.Hour)
	earlier := now.Add(-time.Minute)
	revoked := now.Add(-time.Second)
	for _, tc := range []struct {
		name   string
		stored application.Grant
		in     application.ExtendInput
		kind   admin.Failure
	}{
		{name: "invalid version", in: application.ExtendInput{Version: 0, InternalReason: "gift"}, kind: admin.InvalidInput},
		{name: "invalid reason", in: application.ExtendInput{Version: 1, InternalReason: " "}, kind: admin.InvalidInput},
		{name: "stale", stored: application.Grant{Version: 2}, in: application.ExtendInput{Version: 1, InternalReason: "gift"}, kind: admin.Conflict},
		{name: "revoked", stored: application.Grant{Version: 1, RevokedAt: &revoked}, in: application.ExtendInput{Version: 1, InternalReason: "gift"}, kind: admin.Conflict},
		{name: "indefinite", stored: application.Grant{Version: 1}, in: application.ExtendInput{Version: 1, Indefinite: true, InternalReason: "gift"}, kind: admin.Conflict},
		{name: "same expiry", stored: application.Grant{Version: 1, ExpiresAt: &end}, in: application.ExtendInput{Version: 1, ExpiresAt: &end, InternalReason: "gift"}, kind: admin.InvalidInput},
		{name: "expired extension", stored: application.Grant{Version: 1, ExpiresAt: &earlier}, in: application.ExtendInput{Version: 1, ExpiresAt: &earlier, InternalReason: "gift"}, kind: admin.InvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := tc.stored
			g.ID = key.ID
			g.BusinessID = key.BusinessID
			g.StartsAt = now.Add(-time.Hour)
			svc := application.New(grantAuth(actor), businessReader{}, transaction{grantWriter{grant: g}})
			_, err := svc.Extend(t.Context(), actor, key, tc.in)
			requireGrantFailure(t, err, tc.kind)
		})
	}
}
