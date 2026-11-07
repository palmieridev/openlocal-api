package accessgrants

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
func TestResolveIndependentLevelsAndSources(t *testing.T) {
	now := time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC)
	business := uuid.New()
	sub := db.BusinessSubscription{BusinessID: business, Tier: "essential", Status: "active", Modules: []string{"appointments"}, PeriodStart: ts(now.Add(-time.Hour)), PeriodEnd: ts(now.AddDate(0, 1, 0))}
	grant := db.BusinessAccessGrant{ID: uuid.New(), BusinessID: business, Module: "preorders", Tier: "business", StartsAt: ts(now.Add(-time.Minute)), ExpiresAt: ts(now.Add(time.Hour)), InternalReason: "secret internal", OwnerMessage: sql.NullString{String: "Welcome", Valid: true}}
	for _, tc := range []struct {
		name   string
		mutate func(*db.BusinessAccessGrant)
		want   string
	}{
		{"active", func(*db.BusinessAccessGrant) {}, "business"},
		{"start boundary", func(g *db.BusinessAccessGrant) { g.StartsAt = ts(now) }, "business"},
		{"scheduled", func(g *db.BusinessAccessGrant) { g.StartsAt = ts(now.Add(time.Minute)) }, "free"},
		{"expiry boundary", func(g *db.BusinessAccessGrant) { g.ExpiresAt = ts(now) }, "free"},
		{"expired", func(g *db.BusinessAccessGrant) { g.ExpiresAt = ts(now.Add(-time.Second)) }, "free"},
		{"revoked", func(g *db.BusinessAccessGrant) { g.RevokedAt = ts(now) }, "free"},
		{"indefinite", func(g *db.BusinessAccessGrant) { g.ExpiresAt = pgtype.Timestamptz{} }, "business"},
		{"other business", func(g *db.BusinessAccessGrant) { g.BusinessID = uuid.New() }, "free"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := grant
			tc.mutate(&g)
			got := Resolve(sub, []db.BusinessAccessGrant{g}, "preorders", now)
			if got.Tier != tc.want {
				t.Fatalf("%+v", got)
			}
			a := Resolve(sub, []db.BusinessAccessGrant{g}, "appointments", now)
			if a.Tier != "essential" || a.TeamHistory {
				t.Fatalf("other module upgraded %+v", a)
			}
			data, _ := json.Marshal(got.Sources)
			if strings.Contains(string(data), "secret") || strings.Contains(string(data), "internal_reason") {
				t.Fatal(string(data))
			}
		})
	}
	// A weaker grant cannot lower paid access; revoking a stronger grant restores it.
	grant.Module = "appointments"
	grant.Tier = "business"
	got := Resolve(sub, []db.BusinessAccessGrant{grant, grant}, "appointments", now)
	if got.Tier != "business" || len(got.Sources) != 3 {
		t.Fatalf("%+v", got)
	}
	grant.RevokedAt = ts(now)
	if got = Resolve(sub, []db.BusinessAccessGrant{grant}, "appointments", now); got.Tier != "essential" || len(got.Sources) != 1 || !got.TeamHistory {
		t.Fatalf("%+v", got)
	}
	sub.Tier = "business"
	grant.Tier = "essential"
	grant.RevokedAt = pgtype.Timestamptz{}
	if got = Resolve(sub, []db.BusinessAccessGrant{grant}, "appointments", now); got.Tier != "business" || len(got.Sources) != 2 {
		t.Fatalf("weaker grant reduced paid capability %+v", got)
	}
	sub.Tier = "essential"
	grant.Tier = "business"
	grant.RevokedAt = ts(now)
	grant.StartsAt = ts(now.Add(time.Minute))
	if Resolve(sub, []db.BusinessAccessGrant{grant}, "appointments", now).TeamHistory {
		t.Fatal("scheduled grant granted team history")
	}
}
func TestPilotGraceAndStableWindow(t *testing.T) {
	now := time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC)
	sub := db.BusinessSubscription{BusinessID: uuid.New(), Tier: "business", Status: "pilot", Modules: []string{"appointments", "preorders"}, PeriodStart: ts(now.Add(-time.Hour)), PeriodEnd: ts(now.Add(time.Hour))}
	if !Paid(sub, now) || Resolve(sub, nil, "appointments", now).Sources[0].Kind != "pilot" {
		t.Fatal("pilot lost access")
	}
	start, end := Window(sub, now)
	if !start.Equal(sub.PeriodStart.Time) || !end.Equal(sub.PeriodEnd.Time) {
		t.Fatal("pilot window changed")
	}
	sub.Status = "past_due"
	sub.PeriodEnd = ts(now.Add(-time.Minute))
	sub.GraceUntil = ts(now.Add(time.Hour))
	if !Paid(sub, now) {
		t.Fatal("grace lost")
	}
	sub.GraceUntil = ts(now)
	if Paid(sub, now) {
		t.Fatal("grace boundary")
	}
	edge := time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("+14", 14*60*60))
	emptyStart, _ := Window(db.BusinessSubscription{}, edge)
	if !emptyStart.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("complimentary window followed caller timezone")
	}
	for _, status := range []string{"inactive", "cancelled", "past_due"} {
		sub.Status = status
		start, end = Window(sub, now)
		if !start.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatal("unstable month")
		}
	}
}
func TestCreateInputValidation(t *testing.T) {
	now := time.Now()
	valid := CreateInput{Module: "appointments", Tier: "essential", StartsAt: now, Indefinite: true, InternalReason: "gift"}
	cases := []func(*CreateInput){
		func(v *CreateInput) { v.Module = "pickup" }, func(v *CreateInput) { v.Tier = "free" }, func(v *CreateInput) { v.StartsAt = time.Time{} }, func(v *CreateInput) { v.InternalReason = " " },
		func(v *CreateInput) { v.Indefinite = false }, func(v *CreateInput) { v.ExpiresAt = &now }, func(v *CreateInput) { v.OwnerMessage = new(string) },
		func(v *CreateInput) {
			past := now.Add(-time.Second)
			v.StartsAt = now.Add(-time.Hour)
			v.Indefinite = false
			v.ExpiresAt = &past
		},
	}
	if err := valid.validate(now); err != nil {
		t.Fatal(err)
	}
	for i, mutate := range cases {
		v := valid
		mutate(&v)
		if v.validate(now) == nil {
			t.Fatalf("accepted invalid case %d", i)
		}
	}
}
