package validator

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"net/http/httptest"
)

func TestDecodeStrictRejectsUnknownField(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	app := fiber.New()
	app.Post("/", func(c *fiber.Ctx) error {
		var req request
		if err := DecodeStrict(c, &req); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusOK)
	})

	res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/", strings.NewReader(`{"name":"Cafe","cost":"hidden"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestPageRejectsOverLimit(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		_, _, err := Page(c)
		if err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusOK)
	})

	res, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/?limit=101", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestSlugNormalizesAndValidates(t *testing.T) {
	slug := Slug(" Café Local  123 ")
	if slug != "caf-local-123" {
		t.Fatalf("unexpected slug %q", slug)
	}
	if err := ValidateSlug("bad--slug"); err == nil {
		t.Fatal("expected invalid slug")
	}
}
