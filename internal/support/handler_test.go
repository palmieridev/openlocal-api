package support

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
)

type fakeFeedbackService struct {
	id    uuid.UUID
	req   FeedbackRequest
	calls int
}

func (f *fakeFeedbackService) Create(_ context.Context, req FeedbackRequest) (uuid.UUID, error) {
	f.calls++
	f.req = req
	return f.id, nil
}

func feedbackTestApp(service feedbackService) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	Handler{service: service}.RegisterPublicRoutes(app)
	return app
}

func TestCreateFeedbackReturnsCreatedID(t *testing.T) {
	wantID := uuid.New()
	service := &fakeFeedbackService{id: wantID}
	app := feedbackTestApp(service)
	req := httptest.NewRequest(fiber.MethodPost, "/public/support/feedback", strings.NewReader(
		`{"doc_id":"faq/payments","locale":"es","verdict":"up","comment":null,"path":"/es/faq/payments"}`,
	))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != fiber.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %d, want 201: %s", res.StatusCode, body)
	}
	var payload FeedbackCreatedResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != wantID.String() {
		t.Fatalf("id = %q, want %q", payload.ID, wantID)
	}
	if service.calls != 1 || service.req.DocID != "faq/payments" {
		t.Fatalf("service received %#v in %d calls", service.req, service.calls)
	}
}

func TestCreateFeedbackUsesJSONValidationErrors(t *testing.T) {
	service := &fakeFeedbackService{id: uuid.New()}
	app := feedbackTestApp(service)
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
	if service.calls != 0 {
		t.Fatal("invalid JSON reached service")
	}
}
