package business_test

import (
	"context"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/business"
	"github.com/palmieridev/openlocal-api/internal/business/businesspostgres"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	"github.com/palmieridev/openlocal-api/internal/clerk"
	"testing"
)

type organizationClient struct{ created, deleted []string }

func (o *organizationClient) CreateOrganization(_ context.Context, in clerk.CreateOrganizationInput) (clerk.Organization, error) {
	if in.CreatedBy == "" {
		return clerk.Organization{}, fmt.Errorf("missing creator")
	}
	id := "org_business_test_" + uuid.NewString()
	o.created = append(o.created, id)
	return clerk.Organization{ID: id}, nil
}
func (o *organizationClient) DeleteOrganization(_ context.Context, id string) error {
	o.deleted = append(o.deleted, id)
	return nil
}
func (f *fixture) organizations(o *organizationClient) {
	f.rt.Clerk = o
	f.app = fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	business.NewHandler(f.rt).RegisterPrivateRoutes(f.app.Group("", func(c *fiber.Ctx) error { auth.SetFiber(c, f.identity); return c.Next() }))
}
func failAudit(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `CREATE FUNCTION business_test_reject_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.action LIKE 'business.%' AND EXISTS(SELECT 1 FROM businesses WHERE id=NEW.business_id AND description='business-test-reject-audit') THEN RAISE EXCEPTION 'fixture audit failure'; END IF;RETURN NEW;END $$;
 CREATE TRIGGER business_test_reject_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION business_test_reject_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), "DROP FUNCTION business_test_reject_audit() CASCADE"); err != nil {
			t.Error(err)
		}
	})
}
func TestBusinessCreationCommitsAllLocalEffects(t *testing.T) {
	f := setup(t)
	o := &organizationClient{}
	f.organizations(o)
	slug := "business-create-" + uuid.NewString()
	data := f.request(t, "POST", "/businesses", fmt.Sprintf(`{"name":"Mobile Shop","slug":%q,"status":"active","location_mode":"mobile","service_areas":[{"name":"Coyoacán","country":"mx","state":"CDMX"}]}`, slug), 201)
	b := decode[business.Response](t, data)
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), "DELETE FROM businesses WHERE id=$1", b.ID); err != nil {
			t.Error(err)
		}
	})
	if len(o.created) != 1 || len(o.deleted) != 0 || b.ClerkOrgID != o.created[0] || len(b.ServiceAreas) != 1 {
		t.Fatalf("creation %s provider %+v", data, o)
	}
	var members, locations, audits int
	err := f.pool.QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM business_members WHERE business_id=$1 AND user_id=$2 AND role='owner'),
 (SELECT count(*) FROM inventory_locations WHERE business_id=$1 AND is_default),
 (SELECT count(*) FROM audit_logs WHERE business_id=$1 AND action='business.create')`, b.ID, f.identity.UserID).Scan(&members, &locations, &audits)
	if err != nil {
		t.Fatal(err)
	}
	if members != 1 || locations != 1 || audits != 1 {
		t.Fatalf("effects %d %d %d", members, locations, audits)
	}
}
func TestBusinessCreationRollsBackAndCompensatesOrganization(t *testing.T) {
	f := setup(t)
	o := &organizationClient{}
	f.organizations(o)
	failAudit(t, f)
	slug := "business-rollback-" + uuid.NewString()
	f.request(t, "POST", "/businesses", fmt.Sprintf(`{"name":"Rollback Shop","slug":%q,"description":"business-test-reject-audit"}`, slug), 500)
	var businesses, members int
	err := f.pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM businesses WHERE slug=$1),(SELECT count(*) FROM business_members WHERE clerk_org_id=$2)", slug, o.created[0]).Scan(&businesses, &members)
	if err != nil {
		t.Fatal(err)
	}
	if businesses != 0 || members != 0 || len(o.deleted) != 1 || o.deleted[0] != o.created[0] {
		t.Fatalf("partial effects %d %d provider %+v", businesses, members, o)
	}
}
func TestBusinessMutationsRollBackWithAuditFailure(t *testing.T) {
	f := setup(t)
	b := f.business(t, "draft")
	f.member(t, b, "owner")
	path := "/businesses/" + b.ID.String()
	f.request(t, "PUT", path+"/hours", `{"hours":[{"day_of_week":0,"is_closed":true}]}`, 200)
	if _, err := f.pool.Exec(context.Background(), "UPDATE businesses SET description='business-test-reject-audit' WHERE id=$1", b.ID); err != nil {
		t.Fatal(err)
	}
	failAudit(t, f)
	f.request(t, "PUT", path+"/hours", `{"hours":[{"day_of_week":6,"is_closed":true}]}`, 500)
	f.request(t, "PATCH", path, `{"name":"Changed Name","description":"business-test-reject-audit","location_mode":"mobile","service_areas":[{"name":"Coverage","state":"CDMX"}]}`, 500)
	current := decode[business.Response](t, f.request(t, "GET", path, "", 200))
	hours := decode[business.HoursResponse](t, f.request(t, "GET", path+"/hours", "", 200))
	if current.Name != b.Name || current.LocationMode != "fixed" || len(current.ServiceAreas) != 0 || len(hours.Hours) != 1 || hours.Hours[0].DayOfWeek != 0 {
		t.Fatalf("partial writes %+v %+v", current, hours)
	}
	var audits int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE business_id=$1", b.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("rejected writes created audit rows: %d", audits)
	}
}
func TestBusinessUpdatePreservesReachAndRejectsForeignActor(t *testing.T) {
	f := setup(t)
	b := f.business(t, "draft")
	f.member(t, b, "owner")
	path := "/businesses/" + b.ID.String()
	f.request(t, "PATCH", path, `{"name":"Mobile Shop","location_mode":"mobile","service_areas":[{"name":"Coyoacán","state":"CDMX"}]}`, 200)
	got := decode[business.Response](t, f.request(t, "PATCH", path, `{"name":"Renamed Shop"}`, 200))
	if got.LocationMode != "mobile" || len(got.ServiceAreas) != 1 {
		t.Fatalf("omitted reach lost %+v", got)
	}
	store := businesspostgres.New(f.pool, f.q)
	svc := profile.New(store, nil)
	for _, actor := range []uuid.UUID{uuid.New(), f.identity.UserID} {
		target := b.ID
		if actor == f.identity.UserID {
			target = uuid.New()
		}
		if _, err := svc.Update(context.Background(), profile.Identity{BusinessID: target, ActorID: actor}, profile.Input{Name: "Rejected"}); err != profile.ErrNotFound {
			t.Fatalf("foreign/missing rejection %v", err)
		}
	}
	current := decode[business.Response](t, f.request(t, "GET", path, "", 200))
	if current.Name != "Renamed Shop" {
		t.Fatal("foreign update changed profile")
	}
}
