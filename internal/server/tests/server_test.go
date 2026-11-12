package server_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/server"
)

func secureTestConfig() config.Config {
	return config.Config{
		CORSAllowedOrigins:  []string{"https://app.example"},
		RateLimitWindow:     time.Minute,
		GlobalRateLimitMax:  10,
		PublicRateLimitMax:  10,
		PrivateRateLimitMax: 10,
		WebhookRateLimitMax: 10,
	}
}

func TestGlobalRateLimitReturnsJSONError(t *testing.T) {
	cfg := secureTestConfig()
	cfg.GlobalRateLimitMax = 2
	app := server.New(server.Deps{Config: cfg})

	for requestNumber := 1; requestNumber <= 3; requestNumber++ {
		res, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/healthz", nil))
		if err != nil {
			t.Fatal(err)
		}
		if requestNumber < 3 && res.StatusCode != fiber.StatusOK {
			t.Fatalf("request %d status = %d, want 200", requestNumber, res.StatusCode)
		}
		if requestNumber == 3 {
			if res.StatusCode != fiber.StatusTooManyRequests {
				t.Fatalf("status = %d, want 429", res.StatusCode)
			}
			if contentType := res.Header.Get(fiber.HeaderContentType); !strings.Contains(contentType, fiber.MIMEApplicationJSON) {
				t.Fatalf("content type = %q, want JSON", contentType)
			}
		}
	}
}

func TestWebhookHasStricterRateLimit(t *testing.T) {
	cfg := secureTestConfig()
	cfg.WebhookRateLimitMax = 2
	app := server.New(server.Deps{Config: cfg})

	for requestNumber := 1; requestNumber <= 3; requestNumber++ {
		res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/api/v1/webhooks/clerk", strings.NewReader(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		if requestNumber == 3 && res.StatusCode != fiber.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", res.StatusCode)
		}
	}
}

func TestWebhookLimitDoesNotCountPublicRequests(t *testing.T) {
	cfg := secureTestConfig()
	cfg.WebhookRateLimitMax = 1
	app := server.New(server.Deps{Config: cfg})

	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		res, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/api/v1/marketplace/search?limit=invalid", nil))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("request %d status = %d, want 400", requestNumber, res.StatusCode)
		}
	}
}

func TestPublicLimitDoesNotCountPrivateRequests(t *testing.T) {
	cfg := secureTestConfig()
	cfg.PublicRateLimitMax = 1
	app := server.New(server.Deps{Config: cfg})

	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		res, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/api/v1/me", nil))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("request %d status = %d, want 401", requestNumber, res.StatusCode)
		}
	}
}

func TestSupportFeedbackRouteIsPublic(t *testing.T) {
	app := server.New(server.Deps{Config: secureTestConfig()})
	req := httptest.NewRequest(fiber.MethodPost, "/api/v1/public/support/feedback", strings.NewReader(
		`{"doc_id":"faq","locale":"en","verdict":"up","comment":null,"path":null}`,
	))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 from the intentionally unconfigured test database", res.StatusCode)
	}
}

func TestCORSDoesNotAdvertiseTestAuthHeaders(t *testing.T) {
	app := server.New(server.Deps{Config: secureTestConfig()})
	req := httptest.NewRequest(fiber.MethodOptions, "/healthz", nil)
	req.Header.Set(fiber.HeaderOrigin, "https://app.example")
	req.Header.Set(fiber.HeaderAccessControlRequestMethod, fiber.MethodGet)
	req.Header.Set(fiber.HeaderAccessControlRequestHeaders, "X-Test-Clerk-User-ID")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(res.Header.Get(fiber.HeaderAccessControlAllowHeaders)), "x-test-clerk") {
		t.Fatalf("test auth header was advertised: %q", res.Header.Get(fiber.HeaderAccessControlAllowHeaders))
	}
}

func TestPlatformAdminRoutesRejectBeforeValidationOrDatabaseAccess(t *testing.T) {
	paths := []string{"/api/v1/admin/businesses?limit=invalid", "/api/v1/admin/businesses/not-a-uuid", "/api/v1/admin/activity?business_id=invalid"}
	for _, tc := range []struct {
		name, user, role string
		allowlist        []string
		want             int
	}{
		{name: "anonymous", allowlist: []string{"user_admin"}, want: 401},
		{name: "unverified test header", user: "user_admin", allowlist: []string{"user_admin"}, want: 401},
		{name: "empty allowlist", user: "user_admin", want: 403},
		{name: "organization admin", user: "user_owner", role: "org:admin", allowlist: []string{"user_admin"}, want: 403},
		{name: "allowlisted reaches actor lookup", user: "user_admin", allowlist: []string{"user_admin"}, want: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := secureTestConfig()
			cfg.PlatformAdminUserIDs = tc.allowlist
			app := server.New(server.Deps{Config: cfg, Auth: auth.Middleware{AllowTestAuth: tc.name != "unverified test header"}})
			for _, path := range paths {
				req := httptest.NewRequest("GET", path, nil)
				req.Header.Set("X-Test-Clerk-User-ID", tc.user)
				req.Header.Set("X-Test-Clerk-Org-ID", "org_unrelated")
				req.Header.Set("X-Test-Clerk-Org-Role", tc.role)
				res, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()
				if res.StatusCode != tc.want {
					t.Fatalf("%s status=%d want %d", path, res.StatusCode, tc.want)
				}
			}
		})
	}
}
