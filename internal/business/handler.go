package business

import (
	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	rt api.Runtime
}

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt}
}

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Post("/businesses", h.create)
	private.Get("/businesses/:id", h.get)
	private.Patch("/businesses/:id", h.update)
}

func (h Handler) create(c *fiber.Ctx) error {
	authCtx, user, err := h.rt.CurrentUser(c)
	if err != nil {
		return err
	}
	if authCtx.ClerkOrgID == "" || authCtx.Role == "" {
		return fiber.NewError(fiber.StatusForbidden, "active Clerk organization is required")
	}
	var req Request
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, err := CreateParams(req)
	if err != nil {
		return err
	}
	tx, err := h.rt.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer api.Rollback(c.Context(), tx)
	qtx := h.rt.Q.WithTx(tx)
	business, err := qtx.CreateBusiness(c.Context(), params)
	if err != nil {
		return err
	}
	if _, err := qtx.AddBusinessMember(c.Context(), db.AddBusinessMemberParams{
		BusinessID: business.ID,
		UserID:     user.ID,
		ClerkOrgID: authCtx.ClerkOrgID,
		Role:       "owner",
	}); err != nil {
		return err
	}
	if _, err := qtx.CreateInventoryLocation(c.Context(), db.CreateInventoryLocationParams{
		BusinessID: business.ID,
		Name:       "Default",
		IsDefault:  true,
	}); err != nil {
		return err
	}
	if err := api.Audit(qtx, c.Context(), business.ID, user.ID, "business.create", "business", business.ID); err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(Map(business, true))
}

func (h Handler) get(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "business id")
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, id, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	business, err := h.rt.Q.GetBusinessForMember(c.Context(), db.GetBusinessForMemberParams{ID: id, UserID: user.ID})
	if err != nil {
		return err
	}
	return c.JSON(Map(business, true))
}

func (h Handler) update(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "business id")
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, id, "owner", "manager")
	if err != nil {
		return err
	}
	var req Request
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, err := UpdateParams(id, user.ID, req)
	if err != nil {
		return err
	}
	business, err := h.rt.Q.UpdateBusiness(c.Context(), params)
	if err != nil {
		return err
	}
	_ = api.Audit(h.rt.Q, c.Context(), business.ID, user.ID, "business.update", "business", business.ID)
	return c.JSON(Map(business, true))
}
