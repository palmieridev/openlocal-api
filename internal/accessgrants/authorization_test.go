package accessgrants

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
)

// No database is configured: denial must precede UUID/body parsing and any DB access.
func TestAllGrantRoutesDenyOrdinaryOrgAdmin(t *testing.T) {
	for _, allowed := range [][]string{nil, {"user_platform"}} {
		app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
		NewHandler(api.Runtime{}, allowed).RegisterRoutes(app.Group("/api/v1", auth.Middleware{AllowTestAuth: true}.RequireAuth()))
		for _, route := range []struct{ method, path string }{{"GET", ""}, {"POST", ""}, {"GET", "/bad/history"}, {"POST", "/bad/extend"}, {"POST", "/bad/revoke"}} {
			for _, user := range []string{"", "user_ordinary_owner"} {
				req := httptest.NewRequest(route.method, "/api/v1/admin/businesses/invalid/grants"+route.path, strings.NewReader("bad json"))
				req.Header.Set("X-Test-Clerk-User-ID", user)
				req.Header.Set("X-Test-Clerk-Org-ID", "org_1")
				req.Header.Set("X-Test-Clerk-Org-Role", "org:admin")
				res, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()
				want := 403
				if user == "" {
					want = 401
				}
				if res.StatusCode != want {
					t.Fatalf("%s %s: %d want %d", route.method, route.path, res.StatusCode, want)
				}
			}
		}
	}
}
