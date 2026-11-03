// Package savedbusinesses owns a person's saved storefronts, independently of
// their organization memberships.
package savedbusinesses

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"time"
)

type Handler struct{ rt api.Runtime }

func NewHandler(rt api.Runtime) Handler { return Handler{rt} }
func (h Handler) RegisterRoutes(r fiber.Router) {
	r = r.Group("/me/saved-businesses", func(c *fiber.Ctx) error { c.Set("Cache-Control", "no-store"); return c.Next() })
	r.Get("", h.list)
	r.Get("/:business_id", h.state)
	r.Put("/:business_id", h.save)
	r.Delete("/:business_id", h.remove)
}

type SavedBusiness struct {
	BusinessID uuid.UUID `json:"business_id"`
	SavedAt    time.Time `json:"saved_at"`
	Available  bool      `json:"available"`
	Name       *string   `json:"name"`
	Slug       *string   `json:"slug"`
	LogoURL    *string   `json:"logo_url"`
}
type SavedState struct {
	Saved bool `json:"saved"`
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
		return e
	}
	saved, e := h.rt.Q.IsBusinessSaved(c.Context(), db.IsBusinessSavedParams{UserID: user, BusinessID: id})
	if e != nil {
		return e
	}
	return c.JSON(SavedState{saved})
}
func (h Handler) save(c *fiber.Ctx) error {
	user, id, e := h.identity(c)
	if e != nil {
		return e
	}
	if len(c.Body()) != 0 {
		return api.Fail(400, "invalid_request", "request body must be empty")
	}
	_, e = h.rt.Q.SaveBusiness(c.Context(), db.SaveBusinessParams{UserID: user, BusinessID: id})
	if e != nil {
		return e
	}
	return c.JSON(SavedState{true})
}
func (h Handler) remove(c *fiber.Ctx) error {
	user, id, e := h.identity(c)
	if e != nil {
		return e
	}
	if e = h.rt.Q.RemoveSavedBusiness(c.Context(), db.RemoveSavedBusinessParams{UserID: user, BusinessID: id}); e != nil {
		return e
	}
	return c.SendStatus(204)
}
func (h Handler) list(c *fiber.Ctx) error {
	_, u, e := h.rt.CurrentUser(c)
	if e != nil {
		return e
	}
	scope := "saved:" + u.ID.String()
	cursor, limit, e := api.ParseCursor(c, scope)
	if e != nil {
		return e
	}
	if cursor.Module != "" {
		return api.Fail(400, "invalid_request", "invalid cursor")
	}
	rows, e := h.rt.Q.ListSavedBusinesses(c.Context(), db.ListSavedBusinessesParams{UserID: u.ID, BeforeAt: pgtype.Timestamptz{Time: cursor.At, Valid: !cursor.At.IsZero()}, BeforeID: cursor.ID, PageLimit: limit + 1})
	if e != nil {
		return e
	}
	page := api.CursorPage[SavedBusiness]{Items: []SavedBusiness{}, Limit: limit}
	if len(rows) > int(limit) {
		last := rows[limit-1]
		page.NextCursor = api.EncodeCursor(scope, last.CreatedAt.Time, last.BusinessID, "")
		rows = rows[:limit]
	}
	for _, r := range rows {
		item := SavedBusiness{BusinessID: r.BusinessID, SavedAt: r.CreatedAt.Time, Available: r.Status == "active"}
		if item.Available {
			item.Name = &r.Name
			item.Slug = &r.Slug
			item.LogoURL = api.StringPtr(r.LogoUrl)
		}
		page.Items = append(page.Items, item)
	}
	return c.JSON(page)
}
