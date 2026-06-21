package inventory

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
	private.Post("/inventory/movements", h.createStockMovement)
	private.Get("/inventory/movements", h.listStockMovements)
	private.Get("/inventory/stock-levels", h.listStockLevels)
}

func (h Handler) createStockMovement(c *fiber.Ctx) error {
	var req StockMovementRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	params, businessID, err := MovementParams(req, user.ID)
	if err != nil {
		return err
	}
	if _, err := h.rt.Q.GetVariantForBusiness(c.Context(), db.GetVariantForBusinessParams{ID: params.VariantID, BusinessID: businessID}); err != nil {
		return err
	}
	tx, err := h.rt.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer api.Rollback(c.Context(), tx)
	qtx := h.rt.Q.WithTx(tx)
	locationID, err := ResolveLocation(c.Context(), qtx, businessID, req.LocationID)
	if err != nil {
		return err
	}
	params.LocationID = locationID
	movement, err := qtx.CreateStockMovement(c.Context(), params)
	if err != nil {
		return err
	}
	level, err := qtx.ApplyStockDelta(c.Context(), db.ApplyStockDeltaParams{
		BusinessID:     businessID,
		VariantID:      params.VariantID,
		LocationID:     locationID,
		QuantityOnHand: SignedQuantity(params.MovementType, params.Quantity),
	})
	if err != nil {
		return err
	}
	if err := api.Audit(qtx, c.Context(), businessID, user.ID, "inventory.movement.create", "stock_movement", movement.ID); err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"movement": movement, "stock_level": level})
}

func (h Handler) listStockMovements(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := h.rt.Q.ListStockMovements(c.Context(), db.ListStockMovementsParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (h Handler) listStockLevels(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := h.rt.Q.ListStockLevels(c.Context(), db.ListStockLevelsParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	return c.JSON(items)
}
