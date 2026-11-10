package operations_test

import (
	"bytes"
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
