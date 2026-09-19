package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func verifySignature(header, secret string, body []byte, now time.Time) bool {
	var timestamp string
	var signatures []string
	for _, field := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok {
			continue
		}
		if key == "t" {
			timestamp = value
		}
		if key == "v1" {
			signatures = append(signatures, value)
		}
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || now.Sub(time.Unix(seconds, 0)) > 5*time.Minute || time.Unix(seconds, 0).Sub(now) > 5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		decoded, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(decoded, expected) {
			return true
		}
	}
	return false
}

func (h *Handler) webhook(c *fiber.Ctx) error {
	if h.Config.StripeWebhookSecret == "" || !verifySignature(c.Get("Stripe-Signature"), h.Config.StripeWebhookSecret, c.Body(), time.Now()) {
		return api.Fail(400, "invalid_request", "invalid Stripe signature")
	}
	var event struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				Customer any `json:"customer"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(c.Body(), &event); err != nil || event.ID == "" || event.Type == "" {
		return api.Fail(400, "invalid_request", "invalid Stripe event")
	}
	wanted := strings.HasPrefix(event.Type, "customer.subscription.") || event.Type == "checkout.session.completed" || event.Type == "invoice.paid" || event.Type == "invoice.payment_failed"
	if !wanted {
		return c.SendStatus(fiber.StatusOK)
	}
	customer, ok := event.Data.Object.Customer.(string)
	if !ok || customer == "" {
		return api.Fail(400, "invalid_request", "event customer is missing")
	}
	tx, err := h.RT.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	q := h.RT.Q.WithTx(tx)
	inserted, err := q.SaveBillingEvent(c.Context(), db.SaveBillingEventParams{ID: event.ID, CustomerID: customer})
	if err != nil {
		return err
	}
	if inserted == 1 {
		_, err = q.EnqueueJob(c.Context(), db.EnqueueJobParams{Payload: raw(map[string]any{"type": "reconcile_subscription", "customer_id": customer}), DelaySeconds: 0})
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusOK)
}
