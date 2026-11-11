package validator_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/platform/validator"
)

func TestDecodeStrictRejectsUnknownField(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	app := fiber.New()
	app.Post("/", func(c *fiber.Ctx) error {
		var req request
		if err := validator.DecodeStrict(c, &req); err != nil {
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
		_, _, err := validator.Page(c)
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

func TestPageRejectsMalformedIntegers(t *testing.T) {
	for _, query := range []string{"limit=abc", "offset=1.5"} {
		t.Run(query, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error {
				_, _, err := validator.Page(c)
				return err
			})

			res, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/?"+query, nil))
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("expected 400, got %d", res.StatusCode)
			}
		})
	}
}

func TestGeographicKeysNormalizeAccentsAndFormatting(t *testing.T) {
	if got := validator.SearchKey("  Coyoacán, CDMX  "); got != "coyoacan-cdmx" {
		t.Fatalf("validator.SearchKey() = %q", got)
	}
	if validator.SearchKey("México") != validator.SearchKey("Mexico") {
		t.Fatal("accent variants should have the same key")
	}
	if got := validator.PostalKey(" 06 700 "); got != "06700" {
		t.Fatalf("validator.PostalKey() = %q", got)
	}
}

func TestIdempotencyKeyValidation(t *testing.T) {
	if _, err := validator.IdempotencyKey("movement:01HZY8Q8Q4N7ZB7M2D92VFPD3T"); err != nil {
		t.Fatalf("expected key to pass: %v", err)
	}
	for _, value := range []string{"", "short", "contains spaces"} {
		if _, err := validator.IdempotencyKey(value); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}

func TestSlugNormalizesAndValidates(t *testing.T) {
	slug := validator.Slug(" Café Local  123 ")
	if slug != "caf-local-123" {
		t.Fatalf("unexpected slug %q", slug)
	}
	if err := validator.ValidateSlug("bad--slug"); err == nil {
		t.Fatal("expected invalid slug")
	}
}
