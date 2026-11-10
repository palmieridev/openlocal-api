package billing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/billing"
	"github.com/palmieridev/openlocal-api/internal/config"
)

func TestSignedWebhookDeduplicatesAndRollsBackFailedEnqueue(t *testing.T) {
	pool, _, _ := database(t)
	customer, event := "cus_fixture_enqueue_failure", "evt_"+uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM pgmq.q_openlocal_jobs WHERE message->>'customer_id'=$1", customer)
		_, _ = pool.Exec(context.Background(), "DELETE FROM billing_events WHERE id=$1", event)
	})
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	billing.NewHandler(api.NewRuntime(nil, pool, nil), config.Config{StripeWebhookSecret: "whsec_local"}).RegisterWebhookRoutes(app)
	body := fmt.Sprintf(`{"id":%q,"type":"invoice.paid","data":{"object":{"customer":%q,"private_provider_field":"never enqueue"}}}`, event, customer)
	request := func(body, header string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", "/webhooks/stripe", strings.NewReader(body))
		req.Header.Set("Stripe-Signature", header)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("webhook status=%d; want %d", res.StatusCode, want)
		}
	}
	for _, header := range []string{signature(body, "wrong", time.Now()), signature(body, "whsec_local", time.Now().Add(-6*time.Minute)), signature(body, "whsec_local", time.Now().Add(6*time.Minute))} {
		request(body, header, 400)
	}
	request(body+" ", signature(body, "whsec_local", time.Now()), 400)
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM billing_events WHERE id=$1", event).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid signature persisted event: %d %v", count, err)
	}
	// Fail only this fixture's queue insertion. The event insert must roll back.
	if _, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_billing_test_enqueue() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.message->>'customer_id' = 'cus_fixture_enqueue_failure' THEN RAISE EXCEPTION 'fixture enqueue failure'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER reject_billing_test_enqueue BEFORE INSERT ON pgmq.q_openlocal_jobs FOR EACH ROW EXECUTE FUNCTION reject_billing_test_enqueue()`); err != nil {
		t.Fatal(err)
	}
	drop := func() {
		_, err := pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS reject_billing_test_enqueue ON pgmq.q_openlocal_jobs; DROP FUNCTION IF EXISTS reject_billing_test_enqueue()")
		if err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(drop)
	request(body, signature(body, "whsec_local", time.Now()), 500)
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM billing_events WHERE id=$1", event).Scan(&count); err != nil || count != 0 {
		t.Fatalf("enqueue failure left event: %d %v", count, err)
	}
	drop()
	for i := 0; i < 2; i++ {
		request(body, signature(body, "whsec_local", time.Now()), 200)
	}
	var messages [][]byte
	rows, err := pool.Query(t.Context(), "SELECT message FROM pgmq.q_openlocal_jobs WHERE message->>'customer_id'=$1", customer)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var message []byte
		if err = rows.Scan(&message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("duplicate webhook enqueued %d jobs", len(messages))
	}
	var envelope map[string]string
	if err = json.Unmarshal(messages[0], &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 2 || envelope["type"] != "reconcile_subscription" || envelope["customer_id"] != customer {
		t.Fatalf("unsafe queue envelope: %s", messages[0])
	}
}
