package inventory

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

func (h Handler) createStockMovement(c *fiber.Ctx) error {
	var req StockMovementRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	key, err := v.IdempotencyKey(c.Get("Idempotency-Key"))
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
	result, err := h.createMovement.Create(c.Context(), movementInput(req, businessID, user.ID, key))
	if err != nil {
		return movementHTTPError(err)
	}
	if result.Replayed {
		c.Set("Idempotent-Replayed", "true")
	} else {
		c.Status(fiber.StatusCreated)
	}
	return c.JSON(mapMovementResult(result))
}

func movementInput(req StockMovementRequest, businessID, actorID uuid.UUID, key string) movement.Input {
	return movement.Input{BusinessID: businessID, ActorID: actorID, VariantID: req.VariantID, LocationID: req.LocationID,
		MovementType: req.MovementType, Quantity: req.Quantity, UnitCost: req.UnitCost,
		ReferenceType: req.ReferenceType, ReferenceID: req.ReferenceID, Notes: req.Notes, IdempotencyKey: key}
}
func movementHTTPError(err error) error {
	var validation *movement.ValidationError
	if errors.As(err, &validation) {
		return fiber.NewError(fiber.StatusBadRequest, validation.Error())
	}
	if errors.Is(err, movement.ErrConflict) {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	return err
}

func mapMovementResult(result movement.Result) MovementCreatedResponse {
	item, level := result.Movement, result.StockLevel
	var cost *string
	if item.UnitCost != nil {
		value := item.UnitCost.String()
		cost = &value
	}
	return MovementCreatedResponse{
		Movement: StockMovementResponse{ID: item.ID, VariantID: item.VariantID, LocationID: item.LocationID,
			MovementType: item.MovementType, Quantity: item.Quantity.String(), UnitCost: cost,
			ReferenceType: item.ReferenceType, ReferenceID: item.ReferenceID, Notes: item.Notes, CreatedAt: item.CreatedAt},
		StockLevel: StockLevelResponse{VariantID: level.VariantID, LocationID: level.LocationID,
			QuantityOnHand: level.QuantityOnHand.String(), QuantityReserved: level.QuantityReserved.String(), UpdatedAt: level.UpdatedAt},
	}
}
