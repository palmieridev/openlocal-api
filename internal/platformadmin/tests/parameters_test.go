package platformadmin_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchAndActivityParameters(t *testing.T) {
	pool := adminPool(t)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	q := db.New(tx)
	actor := createUser(t, t.Context(), q)
	for _, tc := range []struct {
		path     string
		activity bool
		want     int
	}{
		{"/?q=small+shop", false, 200},
		{"/?q=" + strings.Repeat("a", 201), false, 400},
		{"/?limit=0", false, 400}, {"/?limit=101", false, 400}, {"/?limit=no", false, 400},
		{"/?cursor=invalid", false, 400}, {"/?cursor=" + *api.EncodeCursor("admin_activity", time.Now(), uuid.New(), ""), false, 400},
		{"/?limit=100&business_id=" + uuid.NewString() + "&actor_user_id=" + uuid.NewString() + "&action=grant.created", true, 200},
		{"/?business_id=invalid", true, 400}, {"/?actor_user_id=invalid", true, 400},
		{"/?action=" + strings.Repeat("a", 121), true, 400}, {"/?limit=-1", true, 400},
		{"/?cursor=" + *api.EncodeCursor("admin_businesses", time.Now(), uuid.New(), ""), true, 400},
	} {
		t.Run(tc.path, func(t *testing.T) {
			app := adminTestApp(q, actor.ClerkUserID)
			route := "/api/v1/admin/businesses"
			if tc.activity {
				route = "/api/v1/admin/activity"
			}
			req := httptest.NewRequest("GET", route+strings.TrimPrefix(tc.path, "/"), nil)
			req.Header.Set("X-Test-Clerk-User-ID", actor.ClerkUserID)
			res, err := app.Test(req)
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
