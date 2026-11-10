package webhooks

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/identity"
	"github.com/palmieridev/openlocal-api/internal/identity/identitypostgres"
)

type Synchronizer interface {
	Synchronize(context.Context, identity.Event) (identity.Result, error)
}
type Handler struct {
	sync   Synchronizer
	secret string
}

func NewHandler(rt api.Runtime, secret string) Handler {
	return NewHandlerWithSynchronizer(identity.New(identitypostgres.New(rt.Pool, rt.Q)), secret)
}

// NewHandlerWithSynchronizer composes the verified HTTP edge with a use case.
func NewHandlerWithSynchronizer(sync Synchronizer, secret string) Handler {
	return Handler{sync: sync, secret: secret}
}
func (h Handler) handleClerk(c *fiber.Ctx) error {
	verifier, err := NewVerifier(h.secret)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "webhook signing secret is not configured")
	}
	body := c.BodyRaw()
	if err := verifier.Verify(c.Get("svix-id"), c.Get("svix-timestamp"), c.Get("svix-signature"), body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook signature")
	}
	var event eventEnvelope
	if err := json.Unmarshal(body, &event); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook payload")
	}
	if _, err := h.sync.Synchronize(c.UserContext(), event.input()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
