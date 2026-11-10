package entitlements_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
)

type store struct {
	grants   []access.Grant
	usage    int32
	writes   int
	business uuid.UUID
	start    time.Time
	failure  error
}

func (s *store) Grants(context.Context, uuid.UUID, time.Time) ([]access.Grant, error) {
	return s.grants, s.failure
}
func (s *store) Enabled(context.Context, uuid.UUID, string) (bool, error) { return true, nil }
func (s *store) Usage(context.Context, access.Subscription, string, time.Time) (int32, error) {
	return s.usage, nil
}
func (s *store) Consume(_ context.Context, business uuid.UUID, _ string, _ uuid.UUID, start time.Time) error {
	s.writes++
	s.business = business
	s.start = start
	return nil
}
func ptr(t time.Time) *time.Time { return &t }

func TestQuotaDecisionsRejectWithoutConsumingAndUseUTCWindow(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("+14", 14*60*60))
	id := uuid.New()
	for _, tc := range []struct {
		name          string
		grants        []access.Grant
		used          int32
		failure, want error
	}{
		{name: "no access", want: access.ErrUnavailable},
		{name: "foreign grant", grants: []access.Grant{{BusinessID: uuid.New(), Module: "appointments", Tier: "business", StartsAt: now.Add(-time.Hour)}}, want: access.ErrUnavailable},
		{name: "quota", grants: []access.Grant{{BusinessID: id, Module: "appointments", Tier: "essential", StartsAt: now.Add(-time.Hour)}}, used: 50, want: access.ErrQuotaExceeded},
		{name: "grant read failure", failure: errors.New("storage failed")},
		{name: "last confirmation", grants: []access.Grant{{BusinessID: id, Module: "appointments", Tier: "essential", StartsAt: now.Add(-time.Hour)}}, used: 49},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &store{grants: tc.grants, usage: tc.used, failure: tc.failure}
			err := access.New(st).Consume(t.Context(), access.Subscription{BusinessID: id, Tier: "free"}, "appointments", uuid.New(), now)
			want := tc.want
			if tc.failure != nil {
				want = tc.failure
			}
			if !errors.Is(err, want) {
				t.Fatalf("error=%v; want %v", err, want)
			}
			if want != nil {
				if st.writes != 0 {
					t.Fatal("rejection consumed quota")
				}
				return
			}
			if st.writes != 1 || st.business != id || !st.start.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("incorrect consumption: %+v", st)
			}
		})
	}
}
func TestGraceWithoutPeriodEndAndTeamHistoryFallback(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	id := uuid.New()
	sub := access.Subscription{BusinessID: id, Tier: "essential", Status: "past_due", Modules: []string{"appointments"}, PeriodStart: ptr(now.Add(-time.Hour)), GraceUntil: ptr(now.Add(time.Hour))}
	st := &store{grants: []access.Grant{{ID: uuid.New(), BusinessID: id, Module: "appointments", Tier: "business", StartsAt: now.Add(-2 * time.Hour), ExpiresAt: ptr(now.Add(-time.Hour))}}}
	svc := access.New(st)
	r, err := svc.Get(t.Context(), sub, "staff", now)
	if err != nil {
		t.Fatal(err)
	}
	a := r.Modules["appointments"]
	if a.Tier != "essential" || a.Limit != 50 || !a.CanManageExisting || !a.CanOperate || a.CanConfigure || a.CanExport || len(a.Sources) != 1 || !r.PeriodEnd.Equal(*sub.GraceUntil) {
		t.Fatalf("fallback/history changed: %+v", r)
	}
	for _, role := range []string{"owner", "manager", "staff", "unknown"} {
		r, err = svc.Get(t.Context(), sub, role, now.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		a = r.Modules["appointments"]
		if a.Owned || a.CanAcceptNew || a.CanConfigure || a.CanExport || a.Tier != "free" || a.CanManageExisting != (role != "unknown") {
			t.Fatalf("expired access role=%s: %+v", role, a)
		}
	}
}
