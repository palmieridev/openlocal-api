// Package savedbusinesses owns a person's saved storefronts, independently of
// their organization memberships.
package savedbusinesses

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/palmieridev/openlocal-api/internal/savedbusinesses/personal"
	"github.com/palmieridev/openlocal-api/internal/savedbusinesses/savedpostgres"
)

type Handler struct {
	rt      api.Runtime
	service *personal.Service
}

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt, service: personal.New(savedpostgres.New(rt.Q))}
}
func httpError(err error) error {
	if errors.Is(err, personal.ErrNotFound) {
		return fiber.NewError(404, "not found")
	}
	if errors.Is(err, personal.ErrInvalid) {
		return api.Fail(400, "invalid_request", err.Error())
	}
	return err
}

func (h Handler) identity(c *fiber.Ctx) (uuid.UUID, uuid.UUID, error) {
	_, u, e := h.rt.CurrentUser(c)
	if e != nil {
		return uuid.Nil, uuid.Nil, e
	}
	id, e := v.ParseUUID(c.Params("business_id"), "business_id")
	return u.ID, id, e
}
func (h Handler) state(c *fiber.Ctx) error {
	user, id, e := h.identity(c)
	if e != nil {
		return httpError(e)
	}
	saved, e := h.service.State(c.Context(), user, id)
	if e != nil {
		return httpError(e)
	}
	return c.JSON(SavedState{saved})
}
func (h Handler) save(c *fiber.Ctx) error {
	user, id, e := h.identity(c)
	if e != nil {
		return httpError(e)
	}
	if len(c.Body()) != 0 {
		return api.Fail(400, "invalid_request", "request body must be empty")
	}
	e = h.service.Save(c.Context(), user, id)
	if e != nil {
		return httpError(e)
	}
	return c.JSON(SavedState{true})
}
func (h Handler) remove(c *fiber.Ctx) error {
	user, id, e := h.identity(c)
	if e != nil {
		return httpError(e)
	}
	if e = h.service.Remove(c.Context(), user, id); e != nil {
		return httpError(e)
	}
	return c.SendStatus(204)
}
func (h Handler) list(c *fiber.Ctx) error {
	_, u, e := h.rt.CurrentUser(c)
	if e != nil {
		return httpError(e)
	}
	scope := "saved:" + u.ID.String()
	cursor, limit, e := api.ParseCursor(c, scope)
	if e != nil {
		return httpError(e)
	}
	if cursor.Module != "" {
		return api.Fail(400, "invalid_request", "invalid cursor")
	}

	result, e := h.service.List(c.Context(), u.ID, personal.Page{BeforeAt: cursor.At, BeforeID: cursor.ID, Limit: limit})
	if e != nil {
		return httpError(e)
	}
	page := api.CursorPage[SavedBusiness]{Items: []SavedBusiness{}, Limit: limit}
	if result.HasMore {
		last := result.Items[len(result.Items)-1]
		page.NextCursor = api.EncodeCursor(scope, last.SavedAt, last.BusinessID, "")
	}
	for _, r := range result.Items {
		page.Items = append(page.Items, SavedBusiness{BusinessID: r.BusinessID, SavedAt: r.SavedAt, Available: r.Available, Name: r.Name, Slug: r.Slug, LogoURL: r.LogoURL})
	}
	return c.JSON(page)
}
