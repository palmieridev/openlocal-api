package accessgrants_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/application"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/grantpostgres"
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

func TestRejectedGrantActorsLeaveNoState(t *testing.T) {
	f := administrationSetup(t)
	in := application.CreateInput{Module: "appointments", Tier: "essential", StartsAt: time.Now(), Indefinite: true, InternalReason: "gift"}
	for _, actor := range []admin.Actor{
		{ClerkUserID: f.ordinary.ClerkUserID, UserID: f.ordinary.ID},
		{ClerkUserID: f.actor.ClerkUserID, UserID: f.ordinary.ID},
		{ClerkUserID: f.actor.ClerkUserID, UserID: uuid.New()},
	} {
		_, err := f.service.Create(t.Context(), actor, f.business.ID, in)
		requireGrantFailure(t, err, admin.Forbidden)
	}
	denied := "user_denied_" + uuid.NewString()
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{"POST", "", in}, {"GET", "", nil}, {"GET", "/" + uuid.NewString() + "/history", nil},
		{"POST", "/" + uuid.NewString() + "/extend", application.ExtendInput{Version: 1, Indefinite: true, InternalReason: "gift"}},
		{"POST", "/" + uuid.NewString() + "/revoke", application.RevokeInput{Version: 1, InternalReason: "end"}},
	} {
		code, data := f.request(t, denied, route.method, route.path, route.body)
		if code != 403 || !bytes.Contains(data, []byte(`"code":"platform_admin_required"`)) {
			t.Fatalf("status=%d body=%s", code, data)
		}
	}
	grants, audits, subs := f.counts(t, f.business.ID)
	if grants != 0 || audits != 0 || subs != 0 {
		t.Fatalf("denied writes=%d/%d/%d", grants, audits, subs)
	}
	var count int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM users WHERE clerk_user_id=$1", denied).Scan(&count); err != nil || count != 0 {
		t.Fatalf("denied actor upserted count=%d err=%v", count, err)
	}
	_, err := f.service.Create(t.Context(), f.identity(), uuid.New(), in)
	if !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("missing business err=%v", err)
	}
}

func (f administrationFixture) rejectAudits(t *testing.T, deferred bool) func() {
	t.Helper()
	data, err := os.ReadFile("testdata/audit_failure.sql")
	if err != nil {
		t.Fatal(err)
	}
	name := "grant_fail_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	function := pgx.Identifier{name}.Sanitize()
	trigger := pgx.Identifier{name + "_trigger"}.Sanitize()
	constraint, timing, deferrable := "", "BEFORE", ""
	if deferred {
		constraint = "CONSTRAINT"
		timing = "AFTER"
		deferrable = "DEFERRABLE INITIALLY DEFERRED"
	}
	sql := strings.NewReplacer("{{constraint}}", constraint, "{{timing}}", timing, "{{deferrable}}", deferrable, "{{function}}", function, "{{trigger}}", trigger, "{{business}}", f.business.ID.String()).Replace(string(data))
	if _, err = f.pool.Exec(t.Context(), sql); err != nil {
		t.Fatal(err)
	}
	remove := func() {
		t.Helper()
		_, err := f.pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS "+trigger+" ON audit_logs; DROP FUNCTION IF EXISTS "+function+"()")
		if err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(remove)
	return remove
}
func TestGrantMutationAndAuditRollbackTogether(t *testing.T) {
	t.Run("creation including default subscription", func(t *testing.T) {
		f := administrationSetup(t)
		remove := f.rejectAudits(t, false)
		in := application.CreateInput{Module: "appointments", Tier: "essential", StartsAt: time.Now(), Indefinite: true, InternalReason: "gift"}
		result, err := f.service.Create(t.Context(), f.identity(), f.business.ID, in)
		if err == nil || result.ID != uuid.Nil {
			t.Fatalf("failed create returned %+v err=%v", result, err)
		}
		grants, audits, subs := f.counts(t, f.business.ID)
		if grants != 0 || audits != 0 || subs != 0 {
			t.Fatalf("partial create=%d/%d/%d", grants, audits, subs)
		}
		remove()
		if _, err = f.service.Create(t.Context(), f.identity(), f.business.ID, in); err != nil {
			t.Fatal(err)
		}
		grants, audits, subs = f.counts(t, f.business.ID)
		if grants != 1 || audits != 1 || subs != 1 {
			t.Fatalf("retry=%d/%d/%d", grants, audits, subs)
		}
	})
	t.Run("extension and revocation", func(t *testing.T) {
		f := administrationSetup(t)
		g := f.create(t)
		remove := f.rejectAudits(t, false)
		later := g.ExpiresAt.Add(time.Hour)
		out, err := f.service.Extend(t.Context(), f.identity(), f.key(g.ID), application.ExtendInput{Version: 1, ExpiresAt: &later, InternalReason: "more time"})
		if err == nil || out.ID != uuid.Nil {
			t.Fatalf("extension=%+v err=%v", out, err)
		}
		out, err = f.service.Revoke(t.Context(), f.identity(), f.key(g.ID), application.RevokeInput{Version: 1, InternalReason: "end"})
		if err == nil || out.ID != uuid.Nil {
			t.Fatalf("revoke=%+v err=%v", out, err)
		}
		stored, err := f.q.GetAccessGrant(t.Context(), db.GetAccessGrantParams{BusinessID: f.business.ID, ID: g.ID})
		if err != nil || stored.Version != 1 || stored.RevokedAt.Valid || !stored.ExpiresAt.Time.Equal(*g.ExpiresAt) {
			t.Fatalf("partial mutation=%+v err=%v", stored, err)
		}
		grants, audits, _ := f.counts(t, f.business.ID)
		if grants != 1 || audits != 1 {
			t.Fatalf("partial audit=%d/%d", grants, audits)
		}
		remove()
		out, err = f.service.Revoke(t.Context(), f.identity(), f.key(g.ID), application.RevokeInput{Version: 1, InternalReason: "end"})
		if err != nil || out.Version != 2 {
			t.Fatalf("retry=%+v err=%v", out, err)
		}
		history, err := f.service.History(t.Context(), f.identity(), f.key(g.ID), application.Page{Limit: 25})
		if err != nil || len(history.Items) != 2 {
			t.Fatalf("history=%+v err=%v", history, err)
		}
	})
}
func TestCoordinatedGrantVersionConflicts(t *testing.T) {
	for _, operations := range [][2]string{{"extend", "extend"}, {"revoke", "revoke"}, {"extend", "revoke"}} {
		t.Run(strings.Join(operations[:], " versus "), func(t *testing.T) {
			f := administrationSetup(t)
			g := f.create(t)
			later := g.ExpiresAt.Add(time.Hour)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			release, wait := f.hold(t)
			type result struct {
				grant application.Grant
				err   error
			}
			results := make(chan result, 2)
			for _, operation := range operations {
				go func() {
					var out application.Grant
					var err error
					if operation == "extend" {
						out, err = f.service.Extend(ctx, f.identity(), f.key(g.ID), application.ExtendInput{Version: 1, ExpiresAt: &later, InternalReason: "more time"})
					} else {
						out, err = f.service.Revoke(ctx, f.identity(), f.key(g.ID), application.RevokeInput{Version: 1, InternalReason: "end"})
					}
					results <- result{out, err}
				}()
			}
			wait(2)
			release()
			success, conflicts := 0, 0
			for range 2 {
				select {
				case result := <-results:
					if result.err == nil {
						success++
						if result.grant.Version != 2 {
							t.Fatalf("version=%d", result.grant.Version)
						}
					} else {
						requireGrantFailure(t, result.err, admin.Conflict)
						conflicts++
					}
				case <-ctx.Done():
					t.Fatal("grant operations did not finish")
				}
			}
			if success != 1 || conflicts != 1 {
				t.Fatalf("success/conflicts=%d/%d", success, conflicts)
			}
			// Replaying either stale command cannot append another mutation or audit.
			_, err := f.service.Extend(ctx, f.identity(), f.key(g.ID), application.ExtendInput{Version: 1, ExpiresAt: &later, InternalReason: "retry"})
			requireGrantFailure(t, err, admin.Conflict)
			_, err = f.service.Revoke(ctx, f.identity(), f.key(g.ID), application.RevokeInput{Version: 1, InternalReason: "retry"})
			requireGrantFailure(t, err, admin.Conflict)
			stored, err := f.q.GetAccessGrant(ctx, db.GetAccessGrantParams{BusinessID: f.business.ID, ID: g.ID})
			if err != nil || stored.Version != 2 {
				t.Fatalf("stored=%+v err=%v", stored, err)
			}
			history, err := f.service.History(ctx, f.identity(), f.key(g.ID), application.Page{Limit: 25})
			if err != nil || len(history.Items) != 2 {
				t.Fatalf("history=%+v err=%v", history, err)
			}
			for i, item := range history.Items {
				var metadata struct {
					InternalReason string `json:"internal_reason"`
					Before, After  *application.Grant
				}
				if err := json.Unmarshal(item.Metadata, &metadata); err != nil {
					t.Fatal(err)
				}
				if item.ActorUserID == nil || *item.ActorUserID != f.actor.ID || metadata.InternalReason == "" || metadata.After == nil || metadata.After.Version != int32(i+1) {
					t.Fatalf("audit=%s", item.Metadata)
				}
				if i == 0 && metadata.Before != nil || i == 1 && (metadata.Before == nil || metadata.Before.Version != 1) {
					t.Fatalf("before=%s", item.Metadata)
				}
			}
			if history.Items[1].Action == "access_grant.revoked" && !stored.RevokedAt.Valid || history.Items[1].Action == "access_grant.extended" && !stored.ExpiresAt.Time.Equal(later) {
				t.Fatal("history and stored grant disagree")
			}
			grants, audits, _ := f.counts(t, f.business.ID)
			if grants != 1 || audits != 2 {
				t.Fatalf("retry side effects=%d/%d", grants, audits)
			}
		})
	}
}

func TestGrantPersistenceRejectsForeignAndMissingScopedIDs(t *testing.T) {
	f := administrationSetup(t)
	g := f.create(t)
	store := grantpostgres.New(f.pool, f.q)
	later := g.ExpiresAt.Add(time.Hour)
	for _, id := range []uuid.UUID{g.ID, uuid.New()} {
		key := application.ScopedID{BusinessID: f.other.ID, ID: id}
		err := store.WithinBusiness(t.Context(), f.other.ID, func(w application.Writer) error {
			_, err := w.Grant(t.Context(), key)
			if !errors.Is(err, admin.ErrNotFound) {
				t.Fatalf("read scope err=%v", err)
			}
			_, err = w.Extend(t.Context(), application.Extension{Key: key, Version: 1, ExpiresAt: &later})
			if !errors.Is(err, admin.ErrNotFound) {
				t.Fatalf("extension scope err=%v", err)
			}
			_, err = w.Revoke(t.Context(), application.Revocation{Key: key, ActorID: f.actor.ID, Version: 1, Reason: "foreign"})
			if !errors.Is(err, admin.ErrNotFound) {
				t.Fatalf("revocation scope err=%v", err)
			}
			return admin.ErrNotFound
		})
		if !errors.Is(err, admin.ErrNotFound) {
			t.Fatal(err)
		}
	}
	stored, err := f.q.GetAccessGrant(t.Context(), db.GetAccessGrantParams{BusinessID: f.business.ID, ID: g.ID})
	if err != nil || stored.Version != 1 || stored.RevokedAt.Valid || !stored.ExpiresAt.Time.Equal(*g.ExpiresAt) {
		t.Fatalf("foreign mutation stored=%+v err=%v", stored, err)
	}
}

func TestGrantCommitFailureRollsBackAllWrites(t *testing.T) {
	f := administrationSetup(t)
	f.rejectAudits(t, true)
	in := application.CreateInput{Module: "appointments", Tier: "essential", StartsAt: time.Now(), Indefinite: true, InternalReason: "gift"}
	result, err := f.service.Create(t.Context(), f.identity(), f.business.ID, in)
	if err == nil || result.ID != uuid.Nil {
		t.Fatalf("commit failure result=%+v err=%v", result, err)
	}
	grants, audits, subs := f.counts(t, f.business.ID)
	if grants != 0 || audits != 0 || subs != 0 {
		t.Fatalf("commit failure left grant/audit/subscription=%d/%d/%d", grants, audits, subs)
	}
}
func TestGrantPersistenceRollsBackAfterAuditWasWritten(t *testing.T) {
	f := administrationSetup(t)
	g := f.create(t)
	store := grantpostgres.New(f.pool, f.q)
	later := g.ExpiresAt.Add(time.Hour)
	stopped := errors.New("stop after audit")
	err := store.WithinBusiness(t.Context(), f.business.ID, func(w application.Writer) error {
		after, err := w.Extend(t.Context(), application.Extension{Key: f.key(g.ID), Version: 1, ExpiresAt: &later})
		if err != nil {
			return err
		}
		if err = w.Audit(t.Context(), application.Change{BusinessID: f.business.ID, GrantID: g.ID, ActorID: f.actor.ID, Action: "extended", Reason: "later", Before: &g, After: &after}); err != nil {
			return err
		}
		return stopped
	})
	if !errors.Is(err, stopped) {
		t.Fatal(err)
	}
	stored, err := f.q.GetAccessGrant(t.Context(), db.GetAccessGrantParams{BusinessID: f.business.ID, ID: g.ID})
	if err != nil || stored.Version != 1 || !stored.ExpiresAt.Time.Equal(*g.ExpiresAt) {
		t.Fatalf("rollback after audit=%+v err=%v", stored, err)
	}
	grants, audits, _ := f.counts(t, f.business.ID)
	if grants != 1 || audits != 1 {
		t.Fatalf("rollback left grant/audit=%d/%d", grants, audits)
	}
}
