package platformadmin

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

const businessScope = "admin_businesses"
const activityScope = "admin_activity"

type Handler struct {
	RT     api.Runtime
	Config config.Config
}

func NewHandler(rt api.Runtime, cfg config.Config) *Handler { return &Handler{RT: rt, Config: cfg} }
func (h *Handler) RegisterPrivateRoutes(r fiber.Router) {
	// Attach middleware per route so separately registered grant routes do not
	// resolve the actor twice or depend on registration order.
	r.Get("/admin/businesses", h.authorize, h.search)
	r.Get("/admin/businesses/:id", h.authorize, h.detail)
	r.Get("/admin/activity", h.authorize, h.activity)
}
func (h *Handler) authorize(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	if _, _, err := h.RT.RequirePlatformAdmin(c, h.Config.PlatformAdminUserIDs); err != nil {
		return err
	}
	return c.Next()
}
func searchParams(c *fiber.Ctx) (db.AdminSearchBusinessesParams, int32, error) {
	cursor, limit, err := api.ParseCursor(c, businessScope)
	if err != nil {
		return db.AdminSearchBusinessesParams{}, 0, err
	}
	query := strings.TrimSpace(c.Query("q"))
	if err := v.StringLength(query, "q", 0, 200); err != nil {
		return db.AdminSearchBusinessesParams{}, 0, err
	}
	return db.AdminSearchBusinessesParams{SearchQuery: query, BeforeAt: pgtype.Timestamptz{Time: cursor.At, Valid: !cursor.At.IsZero()}, BeforeID: cursor.ID, LimitCount: limit + 1}, limit, nil
}
func (h *Handler) search(c *fiber.Ctx) error {
	params, limit, err := searchParams(c)
	if err != nil {
		return err
	}
	rows, err := h.RT.Q.AdminSearchBusinesses(c.Context(), params)
	if err != nil {
		return err
	}
	page := api.CursorPage[BusinessResponse]{Items: make([]BusinessResponse, 0, len(rows)), Limit: limit}
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		page.NextCursor = api.EncodeCursor(businessScope, api.TS(last.CreatedAt), last.ID, "")
	}
	for _, row := range rows {
		page.Items = append(page.Items, mapBusiness(db.AdminGetBusinessRow(row)))
	}
	return c.JSON(page)
}
func (h *Handler) detail(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "id")
	if err != nil {
		return err
	}
	business, err := h.RT.Q.AdminGetBusiness(c.Context(), id)
	if err != nil {
		return err
	}
	subscription, err := h.RT.Q.GetSubscription(c.Context(), id)
	response := BusinessDetailResponse{Business: mapBusiness(business), AccessRole: "owner"}
	if errors.Is(err, pgx.ErrNoRows) {
		// Inspection must not create subscriptions. Missing subscription is explicitly
		// null. Grants and UTC-month usage can still contribute to effective access.
		subscription = db.BusinessSubscription{BusinessID: id, Tier: "free", Status: "inactive"}
	} else if err != nil {
		return err
	} else {
		response.Subscription = mapSubscription(subscription)
	}
	access, err := entitlements.Get(c.Context(), h.RT.Q, subscription, "owner", time.Now())
	if err != nil {
		return err
	}
	response.Modules = mapModules(access)
	return c.JSON(response)
}
func optionalID(c *fiber.Ctx, key string) (uuid.NullUUID, error) {
	raw := c.Query(key)
	if raw == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := v.ParseUUID(raw, key)
	return uuid.NullUUID{UUID: id, Valid: err == nil}, err
}
func activityParams(c *fiber.Ctx) (db.AdminListActivityParams, int32, error) {
	cursor, limit, err := api.ParseCursor(c, activityScope)
	if err != nil {
		return db.AdminListActivityParams{}, 0, err
	}
	businessID, err := optionalID(c, "business_id")
	if err != nil {
		return db.AdminListActivityParams{}, 0, err
	}
	actorID, err := optionalID(c, "actor_user_id")
	if err != nil {
		return db.AdminListActivityParams{}, 0, err
	}
	action := strings.TrimSpace(c.Query("action"))
	if err := v.StringLength(action, "action", 0, 120); err != nil {
		return db.AdminListActivityParams{}, 0, err
	}
	return db.AdminListActivityParams{BusinessID: businessID, ActorUserID: actorID, ActionFilter: action, BeforeAt: pgtype.Timestamptz{Time: cursor.At, Valid: !cursor.At.IsZero()}, BeforeID: cursor.ID, LimitCount: limit + 1}, limit, nil
}
func (h *Handler) activity(c *fiber.Ctx) error {
	params, limit, err := activityParams(c)
	if err != nil {
		return err
	}
	rows, err := h.RT.Q.AdminListActivity(c.Context(), params)
	if err != nil {
		return err
	}
	page := api.CursorPage[ActivityResponse]{Items: make([]ActivityResponse, 0, len(rows)), Limit: limit}
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		page.NextCursor = api.EncodeCursor(activityScope, api.TS(last.CreatedAt), last.ID, "")
	}
	for _, row := range rows {
		page.Items = append(page.Items, mapActivity(row))
	}
	return c.JSON(page)
}
