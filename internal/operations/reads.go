package operations

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
)

func notFound() error { return api.Fail(404, "not_found", "request not found") }
func (h *Handler) detail(c *fiber.Ctx) error {
	actor, err := h.actor(c, false, c.Params("module"))
	if err != nil {
		return err
	}
	raw := c.Params("record_id")
	if actor.Public {
		c.Set("Cache-Control", "no-store")
		raw = c.Params("public_id")
	}
	id, err := parseID(raw)
	if err != nil {
		if actor.Public {
			return notFound()
		}
		return err
	}
	out, err := h.service().Detail(c.Context(), application.ReadInput{Actor: actor, ID: id, Module: c.Params("module"), Token: c.Get("X-Openlocal-Management-Token")})
	if out.Personal != nil {
		return send(c, *out.Personal, err)
	}
	return send(c, out.Record, err)
}
func (h *Handler) publicRecord(c *fiber.Ctx) error   { return h.detail(c) }
func (h *Handler) ownerRecord(c *fiber.Ctx) error    { return h.detail(c) }
func (h *Handler) personalDetail(c *fiber.Ctx) error { return h.detail(c) }
func (h *Handler) publicServices(c *fiber.Ctx) error {
	result, err := h.service().Services(c.Context(), application.Actor{Slug: c.Params("slug"), Public: true, Role: "customer"})
	return send(c, result, err)
}
func (h *Handler) availability(c *fiber.Ctx) error {
	actor, err := h.actor(c, false, "appointments")
	if err != nil {
		return err
	}
	result, err := h.service().Availability(c.Context(), application.SelectionInput{Actor: actor, ServiceID: c.Query("service_id"), ResourceID: c.Query("resource_id"), Date: c.Query("date")})
	return send(c, result, err)
}
func (h *Handler) publicCatalog(c *fiber.Ctx) error {
	actor, err := h.actor(c, false, "preorders")
	if err != nil {
		return err
	}
	result, err := h.service().Catalog(c.Context(), application.SelectionInput{Actor: actor})
	return send(c, result, err)
}
func (h *Handler) pickupWindows(c *fiber.Ctx) error {
	actor, err := h.actor(c, false, "preorders")
	if err != nil {
		return err
	}
	result, err := h.service().PickupWindows(c.Context(), application.SelectionInput{Actor: actor, Date: c.Query("date")})
	return send(c, result, err)
}
func (h *Handler) list(c *fiber.Ctx) error {
	actor, err := h.authorize(c, false)
	if err != nil {
		return err
	}
	filters := application.Filters{Status: c.Query("status"), Limit: 25}
	limit, err := strconv.Atoi(c.Query("limit", "25"))
	if err != nil || limit < 1 || limit > 100 {
		return bad("limit must be 1 to 100")
	}
	filters.Limit = int32(limit)
	offset, err := strconv.Atoi(c.Query("offset", "0"))
	if err != nil || offset < 0 || offset > 100000 {
		return bad("invalid offset")
	}
	filters.Offset = int32(offset)
	for name, dst := range map[string]*uuid.NullUUID{"resource_id": &filters.Resource, "customer_id": &filters.Customer} {
		if raw := c.Query(name); raw != "" {
			id, err := parseID(raw)
			if err != nil {
				return err
			}
			*dst = uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
		}
	}
	result, err := h.service().List(c.Context(), application.ListInput{Actor: actor, Module: c.Params("module"), Date: c.Query("date"), Filters: filters})
	return send(c, result, err)
}
func (h *Handler) customerHistory(c *fiber.Ctx) error {
	actor, err := h.authorize(c, false)
	if err != nil {
		return err
	}
	id, err := parseID(c.Params("customer_id"))
	if err != nil {
		return err
	}
	result, err := h.service().CustomerHistory(c.Context(), actor, id)
	return send(c, result, err)
}
func (h *Handler) summary(c *fiber.Ctx) error {
	actor, err := h.authorize(c, true, "preorders")
	if err != nil {
		return err
	}
	result, err := h.service().Summary(c.Context(), actor, c.Query("date"))
	return send(c, result, err)
}
func (h *Handler) export(c *fiber.Ctx) error {
	actor, err := h.authorize(c, true)
	if err != nil {
		return err
	}
	result, err := h.service().Export(c.Context(), actor, c.Params("module"), c.Query("from"), c.Query("to"))
	if err != nil {
		return httpError(err)
	}
	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", "attachment; filename=requests.csv")
	return c.SendString(result)
}
func (h *Handler) personalActivity(c *fiber.Ctx) error {
	module := c.Query("module")
	if module != "" && !validModule(module) {
		return bad("invalid module")
	}
	user := personalUser(c)
	scope := "activity:" + user.String() + ":" + module
	cursor, limit, err := api.ParseCursor(c, scope)
	if err != nil {
		return err
	}
	result, err := h.service().Activity(c.Context(), application.ActivityQuery{UserID: user, Module: module, BeforeAt: cursor.At, BeforeID: cursor.ID, BeforeModule: cursor.Module, Limit: limit})
	if err != nil {
		return httpError(err)
	}
	page := api.CursorPage[ActivityItem]{Items: result.Items, Limit: limit}
	if next := result.Next; next != nil {
		page.NextCursor = api.EncodeCursor(scope, next.At, next.ID, next.Module)
	}
	return c.JSON(page)
}
