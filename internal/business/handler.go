package business

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/clerk"
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
	private.Get("/businesses/me", h.getMe)
	private.Get("/businesses/:id", h.get)
	private.Patch("/businesses/:id", h.update)
	private.Get("/businesses/:id/hours", h.getHours)
	private.Put("/businesses/:id/hours", h.replaceHours)
}

func (h Handler) create(c *fiber.Ctx) error {
	authCtx, user, err := h.rt.CurrentUser(c)
	if err != nil {
		return err
	}
	var req Request
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, err := CreateParams(req)
	if err != nil {
		return err
	}
	if h.rt.Clerk == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "clerk organization client is not configured")
	}
	org, err := h.rt.Clerk.CreateOrganization(c.Context(), clerk.CreateOrganizationInput{
		Name:      params.Name,
		Slug:      params.Slug,
		CreatedBy: authCtx.ClerkUserID,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "failed to create Clerk organization")
	}
	tx, err := h.rt.Pool.Begin(c.Context())
	if err != nil {
		_ = h.rt.Clerk.DeleteOrganization(c.Context(), org.ID)
		return err
	}
	defer api.Rollback(c.Context(), tx)
	qtx := h.rt.Q.WithTx(tx)
	params.ClerkOrgID = api.NullString(&org.ID)
	business, err := qtx.CreateBusiness(c.Context(), params)
	if err != nil {
		_ = h.rt.Clerk.DeleteOrganization(c.Context(), org.ID)
		return err
	}
	if _, err := qtx.AddBusinessMember(c.Context(), db.AddBusinessMemberParams{
		BusinessID: business.ID,
		UserID:     user.ID,
		ClerkOrgID: org.ID,
		Role:       "owner",
	}); err != nil {
		_ = h.rt.Clerk.DeleteOrganization(c.Context(), org.ID)
		return err
	}
	if _, err := qtx.CreateInventoryLocation(c.Context(), db.CreateInventoryLocationParams{
		BusinessID: business.ID,
		Name:       "Default",
		IsDefault:  true,
	}); err != nil {
		_ = h.rt.Clerk.DeleteOrganization(c.Context(), org.ID)
		return err
	}
	if err := api.Audit(qtx, c.Context(), business.ID, user.ID, "business.create", "business", business.ID); err != nil {
		_ = h.rt.Clerk.DeleteOrganization(c.Context(), org.ID)
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		_ = h.rt.Clerk.DeleteOrganization(c.Context(), org.ID)
		return err
	}
	response := Map(business, true)
	response.ClerkOrgID = org.ID
	return c.Status(fiber.StatusCreated).JSON(response)
}

func (h Handler) getMe(c *fiber.Ctx) error {
	authCtx, user, err := h.rt.CurrentUser(c)
	if err != nil {
		return err
	}
	if authCtx.ClerkOrgID == "" {
		return fiber.NewError(fiber.StatusForbidden, "active Clerk organization is required")
	}
	if !api.RoleAllowed(authCtx.Role, "owner", "manager", "staff") {
		return fiber.NewError(fiber.StatusForbidden, "role is not allowed")
	}
	businessID, err := h.rt.Q.GetBusinessIDByClerkOrgID(c.Context(), authCtx.ClerkOrgID)
	if err != nil {
		return err
	}
	role, err := h.rt.Q.GetBusinessMemberRole(c.Context(), db.GetBusinessMemberRoleParams{
		BusinessID: businessID,
		UserID:     user.ID,
		ClerkOrgID: authCtx.ClerkOrgID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusForbidden, "business membership is required")
	}
	if err != nil {
		return err
	}
	if !api.RoleAllowed(role, "owner", "manager", "staff") {
		return fiber.NewError(fiber.StatusForbidden, "role is not allowed")
	}
	business, err := h.rt.Q.GetBusinessForMember(c.Context(), db.GetBusinessForMemberParams{ID: businessID, UserID: user.ID})
	if err != nil {
		return err
	}
	return c.JSON(Map(business, true))
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

func (h Handler) getHours(c *fiber.Ctx) error {
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
	hours, err := h.rt.Q.ListBusinessHours(c.Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(MapHours(business.Timezone, hours))
}

func (h Handler) replaceHours(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "business id")
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, id, "owner", "manager")
	if err != nil {
		return err
	}
	var req HoursRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, err := HoursParams(id, req)
	if err != nil {
		return err
	}

	tx, err := h.rt.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer api.Rollback(c.Context(), tx)
	qtx := h.rt.Q.WithTx(tx)
	if err := qtx.DeleteBusinessHours(c.Context(), id); err != nil {
		return err
	}
	for _, param := range params {
		if _, err := qtx.UpsertBusinessHour(c.Context(), param); err != nil {
			return err
		}
	}
	if err := api.Audit(qtx, c.Context(), id, user.ID, "business.hours.update", "business", id); err != nil {
		return err
	}
	hours, err := qtx.ListBusinessHours(c.Context(), id)
	if err != nil {
		return err
	}
	business, err := qtx.GetBusinessForMember(c.Context(), db.GetBusinessForMemberParams{ID: id, UserID: user.ID})
	if err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.JSON(MapHours(business.Timezone, hours))
}
