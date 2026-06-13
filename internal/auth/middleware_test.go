package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRequireAuthRejectsMissingBearerToken(t *testing.T) {
	app := fiber.New()
	app.Get("/private", Middleware{}.RequireAuth(), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	res, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/private", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestRequireAuthTestBypassWithoutOrgHasNoRole(t *testing.T) {
	app := fiber.New()
	app.Get("/private", Middleware{AllowTestAuth: true}.RequireAuth(), func(c *fiber.Ctx) error {
		authCtx, ok := FromFiber(c)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized)
		}
		if authCtx.ClerkOrgID == "" || authCtx.Role == "" {
			return fiber.NewError(fiber.StatusForbidden)
		}
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(fiber.MethodGet, "/private", nil)
	req.Header.Set("X-Test-Clerk-User-ID", "user_test")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.StatusCode)
	}
}

func TestMapClerkRole(t *testing.T) {
	tests := map[string]string{
		"org:admin":  "owner",
		"admin":      "owner",
		"manager":    "manager",
		"org:member": "staff",
		"member":     "staff",
		"unknown":    "",
	}
	for input, want := range tests {
		if got := MapClerkRole(input); got != want {
			t.Fatalf("MapClerkRole(%q) = %q, want %q", input, got, want)
		}
	}
}
