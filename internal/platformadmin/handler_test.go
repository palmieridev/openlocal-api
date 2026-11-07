package platformadmin

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestSearchAndActivityParameters(t *testing.T) {
	for _, tc := range []struct {
		path     string
		activity bool
		want     int
	}{
		{"/?q=small+shop", false, 200},
		{"/?q=" + strings.Repeat("a", 201), false, 400},
		{"/?limit=0", false, 400}, {"/?limit=101", false, 400}, {"/?limit=no", false, 400},
		{"/?cursor=invalid", false, 400}, {"/?cursor=" + *api.EncodeCursor(activityScope, time.Now(), uuid.New(), ""), false, 400},
		{"/?limit=100&business_id=" + uuid.NewString() + "&actor_user_id=" + uuid.NewString() + "&action=grant.created", true, 200},
		{"/?business_id=invalid", true, 400}, {"/?actor_user_id=invalid", true, 400},
		{"/?action=" + strings.Repeat("a", 121), true, 400}, {"/?limit=-1", true, 400},
		{"/?cursor=" + *api.EncodeCursor(businessScope, time.Now(), uuid.New(), ""), true, 400},
	} {
		t.Run(tc.path, func(t *testing.T) {
			app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
			app.Get("/", func(c *fiber.Ctx) error {
				if tc.activity {
					p, limit, err := activityParams(c)
					if err != nil {
						return err
					}
					if p.LimitCount != limit+1 {
						t.Error("missing lookahead")
					}
				} else {
					p, limit, err := searchParams(c)
					if err != nil {
						return err
					}
					if p.LimitCount != limit+1 {
						t.Error("missing lookahead")
					}
					if c.Query("q") == "small shop" && p.SearchQuery != "small shop" {
						t.Error("query changed")
					}
				}
				return c.SendStatus(200)
			})
			res, err := app.Test(httptest.NewRequest("GET", tc.path, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.want {
				t.Fatalf("status=%d want %d", res.StatusCode, tc.want)
			}
		})
	}
}
func TestPrivateDTOsUseExplicitJSONShapes(t *testing.T) {
	modules := mapModules(entitlements.Result{Modules: map[string]entitlements.Access{"appointments": {Owned: true, Usage: 4, Limit: 50, Reason: "intake_paused"}}})
	if a := modules["appointments"]; !a.Owned || a.Usage != 4 || a.Limit != 50 || a.Reason != "intake_paused" {
		t.Fatalf("module=%+v", a)
	}
	detail := BusinessDetailResponse{Business: mapBusiness(db.AdminGetBusinessRow{ID: uuid.New(), Name: "Private"}), AccessRole: "owner", Modules: modules}
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"clerk_org_id":null`, `"subscription":null`, `"access_role":"owner"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	subscription := mapSubscription(db.BusinessSubscription{Tier: "free"})
	data, err = json.Marshal(subscription)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"modules":[]`, `"stripe_customer_id":null`, `"period_start":null`, `"scheduled_change":null`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	activity := mapActivity(db.AuditLog{Metadata: []byte(`{"reason":"private"}`)})
	data, err = json.Marshal(activity)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"actor_user_id":null`, `"business_id":null`, `"entity_id":null`, `"metadata":{"reason":"private"}`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
}
