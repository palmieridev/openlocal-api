package billing_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/billing"
	"github.com/palmieridev/openlocal-api/internal/config"
)

// These contracts do not require persistence or a live payment provider.
func TestRegisteredPublicContracts(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	h := billing.NewHandler(api.Runtime{}, config.Config{})
	h.RegisterPublicRoutes(app)
	h.RegisterWebhookRoutes(app)
	res, err := app.Test(httptest.NewRequest("GET", "/public/plans", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var plans struct {
		Currency  string `json:"currency"`
		Available bool   `json:"billing_available"`
		Second    string `json:"second_module_price"`
		Plans     []struct {
			ID    string `json:"id"`
			Price string `json:"monthly_price"`
		} `json:"plans"`
	}
	if err := json.NewDecoder(res.Body).Decode(&plans); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || plans.Currency != "MXN" || plans.Available || plans.Second != "49.00" || len(plans.Plans) != 3 || plans.Plans[1].Price != "59.00" || plans.Plans[2].ID != "business" {
		t.Fatalf("changed plans: %+v (%d)", plans, res.StatusCode)
	}
	for _, body := range []string{`{}`, `{"id":"evt_untrusted","type":"invoice.paid","data":{"object":{"customer":"cus_untrusted"}}}`} {
		res, err := app.Test(httptest.NewRequest("POST", "/webhooks/stripe", strings.NewReader(body)))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("unsigned webhook accepted: %d", res.StatusCode)
		}
	}
}
