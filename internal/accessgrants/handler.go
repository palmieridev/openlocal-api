package accessgrants

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/application"
	"github.com/palmieridev/openlocal-api/internal/accessgrants/grantpostgres"
	"github.com/palmieridev/openlocal-api/internal/api"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/palmieridev/openlocal-api/internal/platformadmin/adminpostgres"
	admin "github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

type Handler struct {
	RT                  api.Runtime
	AllowedClerkUserIDs []string
}

func NewHandler(rt api.Runtime, allowed []string) *Handler { return &Handler{rt, allowed} }
func (h *Handler) service() *application.Service {
	store := grantpostgres.New(h.RT.Pool, h.RT.Q)
	return application.New(admin.NewAuthorizer(adminpostgres.New(h.RT.Q), h.AllowedClerkUserIDs), store, store)
}

const actorKey = "accessgrants.actor"

func (h *Handler) authorize(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	identity, user, err := h.RT.RequirePlatformAdmin(c, h.AllowedClerkUserIDs)
	if err != nil {
		return err
	}
	c.Locals(actorKey, admin.Actor{ClerkUserID: identity.ClerkUserID, UserID: user.ID})
	return c.Next()
}
func actor(c *fiber.Ctx) admin.Actor { a, _ := c.Locals(actorKey).(admin.Actor); return a }
func invalid(message string) error   { return api.Fail(400, "invalid_request", message) }
func id(raw string) (uuid.UUID, error) {
	value, err := uuid.Parse(raw)
	if err != nil || value == uuid.Nil {
		return uuid.Nil, invalid("invalid identifier")
	}
	return value, nil
}
func httpError(err error) error {
	if errors.Is(err, admin.ErrNotFound) {
		return fiber.NewError(404, "not found")
	}
	var e *admin.Error
	if errors.As(err, &e) {
		status := 400
		switch e.Kind {
		case admin.Forbidden:
			status = 403
		case admin.Conflict:
			status = 409
		}
		return api.Fail(status, e.Code, e.Message)
	}
	return err
}

// Preserve business-existence rejection before body/pagination decoding. Every
// use case independently checks authorization and scope for non-HTTP callers.
func (h *Handler) business(c *fiber.Ctx) (uuid.UUID, error) {
	business, err := id(c.Params("id"))
	if err != nil {
		return business, err
	}
	return business, httpError(h.service().RequireBusiness(c.Context(), actor(c), business))
}
func (h *Handler) list(c *fiber.Ctx) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	out, err := h.service().List(c.Context(), actor(c), business, application.Page{Limit: limit, Offset: offset})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(out)
}
func (h *Handler) create(c *fiber.Ctx) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	var in CreateInput
	if err = v.DecodeStrict(c, &in); err != nil {
		return err
	}
	out, err := h.service().Create(c.Context(), actor(c), business, in)
	if err != nil {
		return httpError(err)
	}
	return c.Status(201).JSON(out)
}
func (h *Handler) extend(c *fiber.Ctx) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	grant, err := id(c.Params("grant_id"))
	if err != nil {
		return err
	}
	var in ExtendInput
	if err = v.DecodeStrict(c, &in); err != nil {
		return err
	}
	out, err := h.service().Extend(c.Context(), actor(c), application.ScopedID{BusinessID: business, ID: grant}, in)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(out)
}
func (h *Handler) revoke(c *fiber.Ctx) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	grant, err := id(c.Params("grant_id"))
	if err != nil {
		return err
	}
	var in RevokeInput
	if err = v.DecodeStrict(c, &in); err != nil {
		return err
	}
	out, err := h.service().Revoke(c.Context(), actor(c), application.ScopedID{BusinessID: business, ID: grant}, in)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(out)
}
func (h *Handler) history(c *fiber.Ctx) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	grant, err := id(c.Params("grant_id"))
	if err != nil {
		return err
	}
	if _, err = h.service().RequireGrant(c.Context(), actor(c), application.ScopedID{BusinessID: business, ID: grant}); err != nil {
		return httpError(err)
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	out, err := h.service().History(c.Context(), actor(c), application.ScopedID{BusinessID: business, ID: grant}, application.Page{Limit: limit, Offset: offset})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(out)
}
