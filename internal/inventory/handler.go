package inventory

import (
	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	rt             api.Runtime
	createMovement *movement.Service
	maintenance    *movement.Maintenance
	reader         *movement.Reader
}

func (h Handler) updateStockMovement(c *fiber.Ctx) error {
	movementID, err := v.ParseUUID(c.Params("id"), "movement id")
	if err != nil {
		return err
	}
	var req StockMovementRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	idempotencyKey, err := v.IdempotencyKey(c.Get("Idempotency-Key"))
	if err != nil {
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
	result, err := h.maintenance.Edit(c.Context(), movement.EditInput{
		Input: movementInput(req, businessID, user.ID, idempotencyKey), MovementID: movementID,
	})
	if err != nil {
		return movementHTTPError(err)
	}
	if result.Replayed {
		c.Set("Idempotent-Replayed", "true")
	}
	return c.JSON(mapMovementResult(result))
}

func (h Handler) deleteStockMovement(c *fiber.Ctx) error {
	movementID, businessID, err := api.IDAndBusinessFromRequest(c, "movement id")
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	if err := h.maintenance.Delete(c.Context(), movement.DeleteInput{
		BusinessID: businessID, ActorID: user.ID, MovementID: movementID,
	}); err != nil {
		return movementHTTPError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
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
	items, err := h.reader.Movements(c.Context(), movement.ListInput{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return movementHTTPError(err)
	}
	out := make([]StockMovementResponse, 0, len(items))
	for _, item := range items {
		out = append(out, mapStockMovement(item))
	}
	return c.JSON(out)
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
	items, err := h.reader.Levels(c.Context(), movement.ListInput{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return movementHTTPError(err)
	}
	out := make([]StockLevelResponse, 0, len(items))
	for _, item := range items {
		out = append(out, mapStockLevel(item))
	}
	return c.JSON(out)
}
