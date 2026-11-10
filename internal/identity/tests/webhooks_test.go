package identity_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/webhooks"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("OPENLOCAL_IDENTITY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires migrated disposable OPENLOCAL_IDENTITY_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") || !strings.HasPrefix(cfg.ConnConfig.Database, "openlocal_identity_test") {
		t.Fatal("identity tests require an explicit disposable local database")
	}
	cfg.ConnConfig.ConnectTimeout = 3 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func business(t *testing.T, pool *pgxpool.Pool, org string) db.Business {
	t.Helper()
	b, err := db.New(pool).CreateBusiness(context.Background(), db.CreateBusinessParams{ClerkOrgID: sql.NullString{String: org, Valid: org != ""}, Name: "Identity fixture", Slug: "identity-" + uuid.NewString(), BusinessType: "retail", Status: "active", Timezone: "America/Mexico_City", LocationMode: "fixed"})
	must(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM businesses WHERE id=$1", b.ID)
		if err != nil {
			t.Error(err)
		}
	})
	return b
}

var signingKey = []byte("local-webhook-fixture-secret")

func appFor(pool *pgxpool.Pool) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	webhooks.NewHandler(api.NewRuntime(nil, pool, nil), "whsec_"+base64.StdEncoding.EncodeToString(signingKey)).RegisterRoutes(app.Group("/api/v1"))
	return app
}
func send(t *testing.T, app *fiber.App, kind string, data any) int {
	t.Helper()
	body, err := json.Marshal(map[string]any{"type": kind, "data": data})
	must(t, err)
	return sendBody(t, app, body, time.Now(), false)
}
func sendBody(t *testing.T, app *fiber.App, body []byte, at time.Time, forged bool) int {
	t.Helper()
	id, ts := "msg_fixture", strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, signingKey)
	_, _ = fmt.Fprintf(mac, "%s.%s.", id, ts)
	_, _ = mac.Write(body)
	sig := mac.Sum(nil)
	if forged {
		sig[0] ^= 1
	}
	req := httptest.NewRequest("POST", "/api/v1/webhooks/clerk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("svix-id", id)
	req.Header.Set("svix-timestamp", ts)
	req.Header.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString(sig))
	res, err := app.Test(req, 5000)
	must(t, err)
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}
func membership(org, user, role string) map[string]any {
	return map[string]any{"id": "orgmem_fixture", "organization": map[string]string{"id": org}, "public_user_data": map[string]string{"user_id": user, "first_name": "After", "identifier": " member@example.test "}, "role": role}
}
func trackUser(t *testing.T, p *pgxpool.Pool, user string) {
	t.Helper()
	t.Cleanup(func() { must(t, db.New(p).DeleteUserByClerkID(context.Background(), user)) })
}
func count(t *testing.T, p *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	must(t, p.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}
func TestSignedWebhookContracts(t *testing.T) {
	p := testPool(t)
	app := appFor(p)
	ctx := context.Background()
	q := db.New(p)
	user := "user_" + uuid.NewString()
	trackUser(t, p, user)
	data := map[string]any{"id": user, "first_name": " First ", "email_addresses": []map[string]string{{"id": "other", "email_address": "other@example.test"}, {"id": "primary", "email_address": "chosen@example.test"}}, "primary_email_address_id": "primary"}
	if got := send(t, app, "user.created", data); got != 204 {
		t.Fatalf("create = %d", got)
	}
	u, err := q.GetUserByClerkID(ctx, user)
	must(t, err)
	if u.FirstName.String != "First" || u.Email.String != "chosen@example.test" {
		t.Fatalf("profile = %+v", u)
	}
	if got := send(t, app, "user.updated", data); got != 204 {
		t.Fatalf("retry = %d", got)
	}
	if n := count(t, p, "SELECT count(*) FROM users WHERE clerk_user_id=$1", user); n != 1 {
		t.Fatalf("retry count = %d", n)
	}
	orgA, orgB := "org_"+uuid.NewString(), "org_"+uuid.NewString()
	a, b := business(t, p, orgA), business(t, p, orgB)
	for _, org := range []string{orgA, orgB} {
		if got := send(t, app, "organizationMembership.created", membership(org, user, "org:admin")); got != 204 {
			t.Fatalf("membership = %d", got)
		}
	}
	for _, biz := range []db.Business{a, b} {
		role, err := q.GetBusinessMemberRole(ctx, db.GetBusinessMemberRoleParams{BusinessID: biz.ID, UserID: u.ID, ClerkOrgID: biz.ClerkOrgID.String})
		must(t, err)
		if role != "owner" {
			t.Fatalf("role = %s", role)
		}
	}
	if got := send(t, app, "organizationMembership.updated", membership(orgA, user, "org:member")); got != 204 {
		t.Fatalf("update = %d", got)
	}
	role, err := q.GetBusinessMemberRole(ctx, db.GetBusinessMemberRoleParams{BusinessID: b.ID, UserID: u.ID, ClerkOrgID: orgB})
	must(t, err)
	if role != "owner" {
		t.Fatal("other org role changed")
	}
	if got := send(t, app, "organizationMembership.deleted", membership(orgA, user, "")); got != 204 {
		t.Fatalf("delete = %d", got)
	}
	if n := count(t, p, "SELECT count(*) FROM business_members WHERE user_id=$1 AND clerk_org_id=$2", u.ID, orgA); n != 0 {
		t.Fatal("membership not deleted")
	}
	if n := count(t, p, "SELECT count(*) FROM business_members WHERE user_id=$1 AND clerk_org_id=$2", u.ID, orgB); n != 1 {
		t.Fatal("other org membership deleted")
	}
	if got := send(t, app, "organization.deleted", map[string]string{"id": orgA}); got != 204 {
		t.Fatalf("archive = %d", got)
	}
	var status string
	must(t, p.QueryRow(ctx, "SELECT status FROM businesses WHERE id=$1", b.ID).Scan(&status))
	if status != "active" {
		t.Fatal("other org archived")
	}
	must(t, p.QueryRow(ctx, "SELECT status FROM businesses WHERE id=$1", a.ID).Scan(&status))
	if status != "archived" {
		t.Fatal("org not archived")
	}
	if got := send(t, app, "user.deleted", map[string]string{"id": user}); got != 204 {
		t.Fatalf("delete user = %d", got)
	}
	if n := count(t, p, "SELECT count(*) FROM business_members WHERE user_id=$1", u.ID); n != 0 {
		t.Fatal("deleted user retained authorization")
	}
}
func TestInvalidWebhookNeverWrites(t *testing.T) {
	p := testPool(t)
	app := appFor(p)
	user := "user_" + uuid.NewString()
	trackUser(t, p, user)
	body := []byte(fmt.Sprintf(`{"type":"user.created","data":{"id":%q}}`, user))
	for _, tc := range []struct {
		name   string
		at     time.Time
		forged bool
	}{{"forged", time.Now(), true}, {"expired", time.Now().Add(-10 * time.Minute), false}, {"future", time.Now().Add(10 * time.Minute), false}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sendBody(t, app, body, tc.at, tc.forged); got != 400 {
				t.Fatalf("status = %d", got)
			}
			if n := count(t, p, "SELECT count(*) FROM users WHERE clerk_user_id=$1", user); n != 0 {
				t.Fatal("unverified write")
			}
		})
	}
	if got := sendBody(t, app, []byte(`{"type":`), time.Now(), false); got != 400 {
		t.Fatalf("malformed = %d", got)
	}
	if got := send(t, app, "unknown.event", map[string]string{"id": user}); got != 204 {
		t.Fatalf("unknown = %d", got)
	}
	if n := count(t, p, "SELECT count(*) FROM users WHERE clerk_user_id=$1", user); n != 0 {
		t.Fatal("unknown event wrote user")
	}
}
func TestInvalidMembershipCannotGrantAccess(t *testing.T) {
	for _, scenario := range []string{"unknown role", "missing organization", "missing user", "unknown organization"} {
		t.Run(scenario, func(t *testing.T) {
			p := testPool(t)
			org, user := "org_"+uuid.NewString(), "user_"+uuid.NewString()
			business(t, p, org)
			trackUser(t, p, user)
			data := membership(org, user, "org:admin")
			switch scenario {
			case "unknown role":
				data["role"] = "org:unexpected"
			case "missing organization":
				delete(data, "organization")
				data["id"] = org
			case "missing user":
				delete(data, "public_user_data")
				data["id"] = user
			case "unknown organization":
				data["organization"] = map[string]string{"id": "org_unknown"}
			}
			if got := send(t, appFor(p), "organizationMembership.created", data); got != 204 {
				t.Fatalf("status = %d", got)
			}
			if n := count(t, p, "SELECT count(*) FROM users WHERE clerk_user_id=$1", user); n != 0 {
				t.Fatal("invalid membership wrote user / granted access")
			}
		})
	}
}
func TestMembershipFailureRollsBackUser(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			p := testPool(t)
			ctx := context.Background()
			org, user := "org_"+uuid.NewString(), "user_"+uuid.NewString()
			b := business(t, p, org)
			trackUser(t, p, user)
			if existing {
				_, err := db.New(p).UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: user, FirstName: sql.NullString{String: "Before", Valid: true}})
				must(t, err)
			}
			_, err := p.Exec(ctx, "UPDATE businesses SET description=$2 WHERE id=$1", b.ID, "identity-test-reject-membership")
			must(t, err)
			// Keep DDL static and scope fault injection to the marked fixture.
			_, err = p.Exec(ctx, `CREATE FUNCTION reject_identity_test_membership() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM businesses WHERE id = NEW.business_id AND description = 'identity-test-reject-membership') THEN
    RAISE EXCEPTION 'fixture membership failure';
  END IF;
  RETURN NEW;
END $$`)
			must(t, err)
			t.Cleanup(func() {
				_, err := p.Exec(ctx, "DROP TRIGGER IF EXISTS reject_identity_test_membership ON business_members; DROP FUNCTION reject_identity_test_membership()")
				must(t, err)
			})
			_, err = p.Exec(ctx, "CREATE TRIGGER reject_identity_test_membership BEFORE INSERT OR UPDATE ON business_members FOR EACH ROW EXECUTE FUNCTION reject_identity_test_membership()")
			must(t, err)
			if got := send(t, appFor(p), "organizationMembership.created", membership(org, user, "org:member")); got != 500 {
				t.Fatalf("injected failure = %d", got)
			}
			if n := count(t, p, "SELECT count(*) FROM business_members WHERE business_id=$1", b.ID); n != 0 {
				t.Fatal("partial membership")
			}
			u, err := db.New(p).GetUserByClerkID(ctx, user)
			if existing {
				must(t, err)
				if u.FirstName.String != "Before" {
					t.Fatalf("failed sync changed user: %q", u.FirstName.String)
				}
			} else if err != pgx.ErrNoRows {
				t.Fatalf("failed sync retained user: %+v, %v", u, err)
			}
			_, err = p.Exec(ctx, "DROP TRIGGER reject_identity_test_membership ON business_members")
			must(t, err)
			for i := 0; i < 2; i++ {
				if got := send(t, appFor(p), "organizationMembership.created", membership(org, user, "org:member")); got != 204 {
					t.Fatalf("retry = %d", got)
				}
			}
			if n := count(t, p, "SELECT count(*) FROM business_members WHERE business_id=$1", b.ID); n != 1 {
				t.Fatalf("retried membership count = %d", n)
			}
		})
	}
}

func TestMembershipRetainsLegacyOrganizationLookup(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	q := db.New(p)
	b := business(t, p, "")
	org := "org_legacy_" + uuid.NewString()
	ownerID := "user_" + uuid.NewString()
	newID := "user_" + uuid.NewString()
	trackUser(t, p, ownerID)
	trackUser(t, p, newID)
	owner, err := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: ownerID})
	must(t, err)
	_, err = q.AddBusinessMember(ctx, db.AddBusinessMemberParams{BusinessID: b.ID, UserID: owner.ID, ClerkOrgID: org, Role: "owner"})
	must(t, err)
	for i := 0; i < 2; i++ {
		if got := send(t, appFor(p), "organizationMembership.created", membership(org, newID, "manager")); got != 204 {
			t.Fatalf("legacy sync = %d", got)
		}
	}
	u, err := q.GetUserByClerkID(ctx, newID)
	must(t, err)
	role, err := q.GetBusinessMemberRole(ctx, db.GetBusinessMemberRoleParams{BusinessID: b.ID, UserID: u.ID, ClerkOrgID: org})
	must(t, err)
	if role != "manager" {
		t.Fatalf("legacy role = %q", role)
	}
	if n := count(t, p, "SELECT count(*) FROM business_members WHERE business_id=$1 AND user_id=$2", b.ID, u.ID); n != 1 {
		t.Fatalf("retried membership count = %d", n)
	}
}
