package billing

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/config"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	RT     api.Runtime
	Config config.Config
	Stripe *Client
}

func NewHandler(rt api.Runtime, cfg config.Config) *Handler { return &Handler{rt, cfg, newClient(cfg)} }
func httpError(err error) error {
	var domain *application.Error
	if errors.As(err, &domain) {
		status := 500
		switch domain.Kind {
		case "invalid":
			status = 400
		case "forbidden":
			status = 403
		case "conflict":
			status = 409
		case "unavailable":
			status = 503
		}
		return api.Fail(status, domain.Code, domain.Message)
	}
	return err
}
func (h *Handler) owner(c *fiber.Ctx, onlyOwner bool) (application.Actor, error) {
	id, e := uuid.Parse(c.Params("id"))
	if e != nil {
		return application.Actor{}, api.Fail(400, "invalid_request", "invalid business identifier")
	}
	roles := []string{"owner", "manager", "staff"}
	if onlyOwner {
		roles = []string{"owner"}
	}
	a, _, e := h.RT.RequireBusinessRole(c, id, roles...)
	if e != nil {
		return application.Actor{}, e
	}
	role, e := h.RT.Q.GetBusinessMemberRole(c.Context(), db.GetBusinessMemberRoleParams{BusinessID: id, UserID: a.UserID, ClerkOrgID: a.ClerkOrgID})
	return application.Actor{BusinessID: id, UserID: a.UserID, Role: role}, e
}
func (h *Handler) plans(c *fiber.Ctx) error {
	a, b, d := amounts(h.Config)
	return c.JSON(fiber.Map{"currency": "MXN", "second_module_price": cents(d), "billing_available": h.available(), "plans": []fiber.Map{{"id": "free", "monthly_price": "0.00", "confirmation_limit": 0, "max_resources": 0, "team_access": false, "advanced": false}, {"id": "essential", "monthly_price": cents(a), "confirmation_limit": 50, "max_resources": 1, "team_access": false, "advanced": false}, {"id": "business", "monthly_price": cents(b), "confirmation_limit": 1000, "max_resources": 5, "team_access": true, "advanced": true}}})
}

func (h *Handler) access(c *fiber.Ctx) error {
	actor, err := h.owner(c, false)
	if err != nil {
		return err
	}
	r, err := h.service().Access(c.Context(), actor, time.Now())
	if err != nil {
		return httpError(err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(r)
}
func (h *Handler) state(c *fiber.Ctx) error {
	actor, err := h.owner(c, true)
	if err != nil {
		return err
	}
	r, err := h.service().State(c.Context(), actor, time.Now())
	if err != nil {
		return httpError(err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(r)
}

func returnURL(base, locale string) string {
	prefix := ""
	if locale == "en" {
		prefix = "/en"
	}
	return strings.TrimRight(base, "/") + prefix + "/dashboard/billing"
}

func (h *Handler) mutate(c *fiber.Ctx) error {
	actor, err := h.owner(c, true)
	if err != nil {
		return err
	}
	if err = h.requireBilling(); err != nil {
		return err
	}
	key, err := v.IdempotencyKey(c.Get("Idempotency-Key"))
	if err != nil {
		return err
	}
	var input application.ChangeInput
	if err = v.DecodeStrict(c, &input); err != nil {
		return err
	}
	action := c.Path()[strings.LastIndex(c.Path(), "/")+1:]
	r, err := h.service().Mutate(c.Context(), application.Mutation{Actor: actor, Action: action, Key: key, Change: input})
	if err != nil {
		return httpError(err)
	}
	c.Set("Content-Type", "application/json")
	if !r.Replayed {
		c.Set("Cache-Control", "no-store")
	}
	return c.Send(r.Response)
}
