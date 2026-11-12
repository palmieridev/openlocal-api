package support_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/support"
)

func feedbackTestApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	support.NewHandler(api.Runtime{}).RegisterPublicRoutes(app)
	return app
}

func TestCreateFeedbackUsesJSONValidationErrors(t *testing.T) {
	app := feedbackTestApp()
	req := httptest.NewRequest(fiber.MethodPost, "/public/support/feedback", strings.NewReader(
		`{"doc_id":"faq","locale":"es","verdict":"up","unexpected":true}`,
	))

	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
	var payload api.ErrorResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(payload.Error, "invalid json:") {
		t.Fatalf("error = %q, want existing validation error style", payload.Error)
	}
}
