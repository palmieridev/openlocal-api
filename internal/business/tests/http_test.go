package business_test

import (
	"context"
	"fmt"
	"github.com/palmieridev/openlocal-api/internal/business"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (f *fixture) member(t *testing.T, b db.Business, role string) {
	t.Helper()
	_, err := f.q.AddBusinessMember(context.Background(), db.AddBusinessMemberParams{BusinessID: b.ID, UserID: f.identity.UserID, ClerkOrgID: f.identity.ClerkOrgID, Role: role})
	if err != nil {
		t.Fatal(err)
	}
}
func TestBusinessHoursContractsAndAuthorization(t *testing.T) {
	f := setup(t)
	b := f.business(t, "draft")
	f.member(t, b, "owner")
	path := "/businesses/" + b.ID.String() + "/hours"
	empty := decode[business.HoursResponse](t, f.request(t, "GET", path, "", 200))
	if empty.Hours == nil || len(empty.Hours) != 0 || empty.Timezone != "America/Tijuana" {
		t.Fatalf("empty %+v", empty)
	}
	input := `{"hours":[{"day_of_week":0,"opens_at":"22:00","closes_at":"02:00","is_closed":false},{"day_of_week":6,"opens_at":null,"closes_at":null,"is_closed":true}]}`
	got := decode[business.HoursResponse](t, f.request(t, "PUT", path, input, 200))
	if len(got.Hours) != 2 || got.Hours[0].DayOfWeek != 0 || *got.Hours[0].ClosesAt != "02:00" || !got.Hours[1].IsClosed || got.Hours[1].OpensAt != nil || got.Hours[1].ClosesAt != nil {
		t.Fatalf("hours %+v", got)
	}
	f.request(t, "PUT", path, `{"hours":[{"day_of_week":0,"is_closed":true},{"day_of_week":0,"is_closed":true}]}`, 400)
	preserved := decode[business.HoursResponse](t, f.request(t, "GET", path, "", 200))
	if len(preserved.Hours) != 2 {
		t.Fatal("invalid replacement changed hours")
	}
	f.identity.Role = "staff"
	f.member(t, b, "staff")
	f.request(t, "GET", path, "", 200)
	f.request(t, "PUT", path, `{"hours":[]}`, 403)
	other := f.business(t, "draft")
	f.request(t, "GET", "/businesses/"+other.ID.String(), "", 403)
}
func TestConcurrentHoursReplacementDoesNotMergeSchedules(t *testing.T) {
	f := setup(t)
	b := f.business(t, "draft")
	f.member(t, b, "owner")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Scope the trigger to this fixture. Both requests must reach a database lock
	// before release; empty schedules expose delete/insert races without row locks.
	if _, err := f.pool.Exec(ctx, "UPDATE businesses SET description='hours-concurrency-fixture' WHERE id=$1", b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `CREATE FUNCTION business_test_hours_barrier() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF EXISTS(SELECT 1 FROM businesses WHERE id=NEW.business_id AND description='hours-concurrency-fixture') THEN
 PERFORM pg_advisory_xact_lock(71372105011); END IF; RETURN NEW; END $$;
 CREATE TRIGGER business_test_hours_barrier BEFORE INSERT ON business_hours FOR EACH ROW EXECUTE FUNCTION business_test_hours_barrier()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), "DROP FUNCTION business_test_hours_barrier() CASCADE"); err != nil {
			t.Error(err)
		}
	})
	gate, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Release()
	if _, err = gate.Exec(ctx, "SELECT pg_advisory_lock(71372105011)"); err != nil {
		t.Fatal(err)
	}
	defer gate.Exec(context.Background(), "SELECT pg_advisory_unlock(71372105011)")
	done := make(chan error, 2)
	for _, body := range []string{`{"hours":[{"day_of_week":0,"is_closed":true}]}`, `{"hours":[{"day_of_week":6,"is_closed":true}]}`} {
		go func() {
			req := httptest.NewRequest("PUT", "/businesses/"+b.ID.String()+"/hours", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res, err := f.app.Test(req, 9000)
			if err == nil {
				defer res.Body.Close()
				if res.StatusCode != 200 {
					data, _ := io.ReadAll(res.Body)
					err = fmt.Errorf("status %d: %s", res.StatusCode, data)
				}
			}
			done <- err
		}()
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		err := f.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock'", f.pool.Config().ConnConfig.RuntimeParams["application_name"]).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("requests did not overlap at database locks")
		case <-ticker.C:
		}
	}
	if _, err = gate.Exec(ctx, "SELECT pg_advisory_unlock(71372105011)"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := f.q.ListBusinessHours(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("concurrent full replacements merged into %d days; want one complete replacement", len(rows))
	}
}
