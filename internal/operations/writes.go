package operations

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
)

func (h *Handler) create(c *fiber.Ctx) error {
	module := c.Params("module")
	actor, err := h.actor(c, false, module)
	if err != nil {
		return err
	}
	if _, err = h.service().ResolveIntake(c.Context(), actor); err != nil {
		return httpError(err)
	}
	in := application.CreateInput{Actor: actor, Module: module, Replay: application.ReplayInput{Key: c.Get("Idempotency-Key"), Body: c.Body()}}
	if module == "appointments" {
		err = decode(c, &in.Appointment)
	} else {
		err = decode(c, &in.Preorder)
	}
	if err != nil {
		return err
	}
	result, err := h.service().Create(c.Context(), in)
	return write(c, result, err)
}
func (h *Handler) mutate(c *fiber.Ctx) error {
	actor, err := h.actor(c, false, c.Params("module"))
	if err != nil {
		return err
	}
	raw := c.Params("record_id")
	if actor.Public {
		raw = c.Params("public_id")
	}
	id, err := parseID(raw)
	if err != nil {
		if actor.Public {
			return notFound()
		}
		return err
	}
	in := application.MutateInput{Actor: actor, ID: id, Module: c.Params("module"), Token: c.Get("X-Openlocal-Management-Token"), Method: c.Method(), Editing: c.Method() == "PATCH", Replay: application.ReplayInput{Key: c.Get("Idempotency-Key"), Body: c.Body()}}
	if actor.Public || actor.UserID != uuid.Nil {
		if _, err = h.service().ResolveRequest(c.Context(), application.ReadInput{Actor: actor, ID: id, Module: in.Module, Token: in.Token}); err != nil {
			return httpError(err)
		}
	}
	if in.Editing {
		err = decode(c, &in.Edit)
	} else if actor.Public || actor.UserID != uuid.Nil {
		var input struct {
			Version int32 `json:"version"`
		}
		err = decode(c, &input)
		in.Transition = TransitionInput{Status: "cancelled", Version: input.Version}
	} else {
		err = decode(c, &in.Transition)
	}
	if err != nil {
		return err
	}
	result, err := h.service().Mutate(c.Context(), in)
	return write(c, result, err)
}
func (h *Handler) configuration(c *fiber.Ctx) error {
	module := c.Params("module")
	actor, err := h.authorize(c, c.Method() != "GET")
	if err != nil {
		return err
	}
	if c.Method() == "GET" {
		result, err := h.service().Settings(c.Context(), actor, module)
		return send(c, result, err)
	}
	result, err := h.service().Configure(c.Context(), application.ConfigurationInput{Actor: actor, Module: module, Patch: c.Body()})
	return send(c, result, err)
}
func (h *Handler) services(c *fiber.Ctx) error {
	actor, err := h.authorize(c, c.Method() != "GET", "appointments")
	if err != nil {
		return err
	}
	if c.Method() == "GET" {
		result, err := h.service().Services(c.Context(), actor)
		return send(c, result, err)
	}
	in := application.ConfigurationInput{Actor: actor, Module: "appointments", Create: c.Method() == "POST", Patch: c.Body()}
	if !in.Create {
		in.ID, err = parseID(c.Params("service_id"))
		if err != nil {
			return err
		}
	}
	result, err := h.service().SaveService(c.Context(), in)
	return send(c, result, err)
}
func (h *Handler) resources(c *fiber.Ctx) error {
	actor, err := h.authorize(c, c.Method() != "GET", "appointments")
	if err != nil {
		return err
	}
	if c.Method() == "GET" {
		result, err := h.service().Resources(c.Context(), actor)
		return send(c, result, err)
	}
	in := application.ConfigurationInput{Actor: actor, Module: "appointments", Create: c.Method() == "POST", Patch: c.Body()}
	if !in.Create {
		in.ID, err = parseID(c.Params("resource_id"))
		if err != nil {
			return err
		}
	}
	result, err := h.service().SaveResource(c.Context(), in)
	return send(c, result, err)
}
