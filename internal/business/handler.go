package business

import (
	"errors"
	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/business/businessclerk"
	"github.com/palmieridev/openlocal-api/internal/business/businesspostgres"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	rt      api.Runtime
	service *profile.Service
}

func NewHandler(rt api.Runtime) Handler {
	var organizations profile.Organizations
	if rt.Clerk != nil {
		organizations = businessclerk.Organizations{Client: rt.Clerk}
	}
	return Handler{rt: rt, service: profile.New(businesspostgres.New(rt.Pool, rt.Q), organizations)}
}
func httpError(err error) error {
	var validation *profile.ValidationError
	if errors.As(err, &validation) {
		return fiber.NewError(400, validation.Message)
	}
	switch {
	case errors.Is(err, profile.ErrNotFound):
		return fiber.NewError(404, "not found")
	case errors.Is(err, profile.ErrMembership), errors.Is(err, profile.ErrRole), errors.Is(err, profile.ErrOrganization):
		return fiber.NewError(403, err.Error())
	case errors.Is(err, profile.ErrOrganizationsUnavailable):
		return fiber.NewError(503, err.Error())
	case errors.Is(err, profile.ErrOrganizationCreate):
		return fiber.NewError(502, err.Error())
	}
	return err
}
func (h Handler) create(c *fiber.Ctx) error {
	auth, user, err := h.rt.CurrentUser(c)
	if err != nil {
		return err
	}
	var req Request
	if err = v.DecodeStrict(c, &req); err != nil {
		return err
	}
	result, err := h.service.Create(c.Context(), profile.Actor{ID: user.ID, ProviderID: auth.ClerkUserID}, profileInput(req))
	if err != nil {
		return httpError(err)
	}
	return c.Status(201).JSON(profileResponse(result))
}
func (h Handler) getMe(c *fiber.Ctx) error {
	auth, user, err := h.rt.CurrentUser(c)
	if err != nil {
		return err
	}
	result, err := h.service.ActiveBusiness(c.Context(), user.ID, auth.ClerkOrgID, auth.Role)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(profileResponse(result))
}
func (h Handler) identity(c *fiber.Ctx, roles ...string) (profile.Identity, error) {
	id, err := v.ParseUUID(c.Params("id"), "business id")
	if err != nil {
		return profile.Identity{}, err
	}
	_, user, err := h.rt.RequireBusinessRole(c, id, roles...)
	if err != nil {
		return profile.Identity{}, err
	}
	return profile.Identity{BusinessID: id, ActorID: user.ID}, nil
}
func (h Handler) get(c *fiber.Ctx) error {
	id, err := h.identity(c, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	result, err := h.service.Business(c.Context(), id)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(profileResponse(result))
}
func (h Handler) update(c *fiber.Ctx) error {
	id, err := h.identity(c, "owner", "manager")
	if err != nil {
		return err
	}
	var req Request
	if err = v.DecodeStrict(c, &req); err != nil {
		return err
	}
	result, err := h.service.Update(c.Context(), id, profileInput(req))
	if err != nil {
		return httpError(err)
	}
	return c.JSON(profileResponse(result))
}
func (h Handler) getHours(c *fiber.Ctx) error {
	id, err := h.identity(c, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	result, err := h.service.Hours(c.Context(), id)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(scheduleResponse(result))
}
func (h Handler) replaceHours(c *fiber.Ctx) error {
	id, err := h.identity(c, "owner", "manager")
	if err != nil {
		return err
	}
	var req HoursRequest
	if err = v.DecodeStrict(c, &req); err != nil {
		return err
	}
	result, err := h.service.ReplaceHours(c.Context(), id, hourInputs(req))
	if err != nil {
		return httpError(err)
	}
	return c.JSON(scheduleResponse(result))
}
