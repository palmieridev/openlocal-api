package accessgrants_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/application"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	admin "github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

func TestGrantHistoryRejectsMissingOrForeignResourceBeforePagination(t *testing.T) {
	f := administrationSetup(t)
	g := f.create(t)
	for _, grant := range []uuid.UUID{uuid.New(), g.ID} {
		target := f
		target.business = f.other
		code, data := target.request(t, f.actor.ClerkUserID, "GET", "/"+grant.String()+"/history?limit=invalid", nil)
		if code != 404 {
			t.Fatalf("status=%d want=404 body=%s", code, data)
		}
	}
}
func TestGrantServiceRejectsForeignAndMissingIDsWithoutPartialWrites(t *testing.T) {
	f := administrationSetup(t)
	g := f.create(t)
	for _, grant := range []uuid.UUID{g.ID, uuid.New()} {
		key := application.ScopedID{BusinessID: f.other.ID, ID: grant}
		_, err := f.service.Extend(t.Context(), f.identity(), key, application.ExtendInput{Version: 1, Indefinite: true, InternalReason: "wrong business"})
		if !errors.Is(err, admin.ErrNotFound) {
			t.Fatalf("extend err=%v", err)
		}
		_, err = f.service.Revoke(t.Context(), f.identity(), key, application.RevokeInput{Version: 1, InternalReason: "wrong business"})
		if !errors.Is(err, admin.ErrNotFound) {
			t.Fatalf("revoke err=%v", err)
		}
		_, err = f.service.History(t.Context(), f.identity(), key, application.Page{Limit: 25})
		if !errors.Is(err, admin.ErrNotFound) {
			t.Fatalf("history err=%v", err)
		}
	}
	grants, audits, subs := f.counts(t, f.other.ID)
	if grants != 0 || audits != 0 || subs != 0 {
		t.Fatalf("rejected writes grants/audits/subscriptions=%d/%d/%d", grants, audits, subs)
	}
	stored, err := f.q.GetAccessGrant(t.Context(), db.GetAccessGrantParams{BusinessID: f.business.ID, ID: g.ID})
	if err != nil || stored.Version != 1 || stored.RevokedAt.Valid {
		t.Fatalf("foreign edit changed grant=%+v err=%v", stored, err)
	}
}
