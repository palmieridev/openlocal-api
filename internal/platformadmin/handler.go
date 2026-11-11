package platformadmin

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/entitlements/accesspostgres"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/palmieridev/openlocal-api/internal/platformadmin/adminpostgres"
	"github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

const businessScope = "admin_businesses"
const activityScope = "admin_activity"
const actorKey = "platformadmin.actor"

type Handler struct {
	RT     api.Runtime
	Config config.Config
}

func NewHandler(rt api.Runtime, cfg config.Config) *Handler { return &Handler{rt, cfg} }
func (h *Handler) service() *application.Service {
	store := adminpostgres.New(h.RT.Q)
	return application.New(application.NewAuthorizer(store, h.Config.PlatformAdminUserIDs), store, access.New(accesspostgres.New(h.RT.Q)))
}
func (h *Handler) authorize(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	identity, user, err := h.RT.RequirePlatformAdmin(c, h.Config.PlatformAdminUserIDs)
	if err != nil {
		return err
	}
	c.Locals(actorKey, application.Actor{ClerkUserID: identity.ClerkUserID, UserID: user.ID})
	return c.Next()
}
func actor(c *fiber.Ctx) application.Actor { a, _ := c.Locals(actorKey).(application.Actor); return a }
func httpError(err error) error {
	if errors.Is(err, application.ErrNotFound) {
		return fiber.NewError(404, "not found")
	}
	var e *application.Error
	if errors.As(err, &e) {
		status := 400
		if e.Kind == application.Forbidden {
			status = 403
		}
		return api.Fail(status, e.Code, e.Message)
	}
	return err
}
func sendPage[T any](c *fiber.Ctx, scope string, page application.Page[T], err error) error {
	if err != nil {
		return httpError(err)
	}
	out := api.CursorPage[T]{Items: page.Items, Limit: page.Limit}
	if page.Next != nil {
		out.NextCursor = api.EncodeCursor(scope, page.Next.At, page.Next.ID, "")
	}
	return c.JSON(out)
}
func (h *Handler) search(c *fiber.Ctx) error {
	cursor, limit, err := api.ParseCursor(c, businessScope)
	if err != nil {
		return err
	}
	page, err := h.service().Search(c.Context(), actor(c), application.SearchQuery{Query: c.Query("q"), Before: application.Position{At: cursor.At, ID: cursor.ID}, Limit: limit})
	return sendPage(c, businessScope, page, err)
}
func (h *Handler) detail(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "id")
	if err != nil {
		return err
	}
	out, err := h.service().Detail(c.Context(), actor(c), id)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(out)
}
func optionalID(c *fiber.Ctx, key string) (*uuid.UUID, error) {
	raw := c.Query(key)
	if raw == "" {
		return nil, nil
	}
	id, err := v.ParseUUID(raw, key)
	return &id, err
}
func (h *Handler) activity(c *fiber.Ctx) error {
	cursor, limit, err := api.ParseCursor(c, activityScope)
	if err != nil {
		return err
	}
	business, err := optionalID(c, "business_id")
	if err != nil {
		return err
	}
	user, err := optionalID(c, "actor_user_id")
	if err != nil {
		return err
	}
	page, err := h.service().Activity(c.Context(), actor(c), application.ActivityQuery{BusinessID: business, ActorUserID: user, Action: c.Query("action"), Before: application.Position{At: cursor.At, ID: cursor.ID}, Limit: limit})
	return sendPage(c, activityScope, page, err)
}
