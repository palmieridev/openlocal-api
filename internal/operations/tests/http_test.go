package operations_test

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/operations"
)

func TestPublicIntakeReportsUnconfiguredDatabase(t *testing.T) {
	handler := operations.NewHandler(api.NewRuntime(nil, nil, nil), config.Config{})
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	handler.RegisterPublicRoutes(app)
	for _, path := range []string{"/public/businesses/store/appointments/services", "/public/businesses/store/preorders/catalog", "/public/businesses/store/preorders/pickup-windows"} {
		req := httptest.NewRequest("GET", path, nil)
		res, err := app.Test(req)
		must(t, err)
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		must(t, err)
		if res.StatusCode != 503 || bytes.Contains(body, []byte(`"code"`)) {
			t.Fatalf("unconfigured database contract: %d %s", res.StatusCode, body)
		}
	}
}

func TestAccessFailurePrecedesSelectorAndConfigurationIdentifiers(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, err := f.h.RT.Pool.Exec(ctx, "UPDATE business_modules SET enabled=false WHERE business_id=$1", f.b.ID)
	must(t, err)
	code, data := f.request("GET", "/public/businesses/"+f.b.Slug+"/appointments/availability?service_id=invalid", "", nil, false)
	if code != 403 {
		t.Fatalf("paused intake validated identifiers first: %d %s", code, data)
	}
	_, err = f.h.RT.Pool.Exec(ctx, "UPDATE business_subscriptions SET tier='free',status='inactive',modules='{}' WHERE business_id=$1", f.b.ID)
	must(t, err)
	for _, path := range []string{"/services/invalid", "/appointment-resources/invalid"} {
		code, data = f.request("PATCH", "/businesses/"+f.b.ID.String()+path, "", map[string]any{"name": "Changed"}, true)
		if code != 403 {
			t.Fatalf("unavailable configuration validated identifiers first: %d %s", code, data)
		}
	}
}
