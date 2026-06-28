package inventory

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/shopspring/decimal"
)

func ResolveLocation(ctx context.Context, q *db.Queries, businessID uuid.UUID, requested *string) (uuid.UUID, error) {
	if requested != nil && *requested != "" {
		return v.ParseUUID(*requested, "location_id")
	}
	location, err := q.GetDefaultInventoryLocation(ctx, businessID)
	if err == nil {
		return location.ID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	location, err = q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: businessID, Name: "Default", IsDefault: true})
	if err != nil {
		return uuid.Nil, err
	}
	return location.ID, nil
}

func ValidMovementType(value string) bool {
	switch value {
	case "IN_PURCHASE", "IN_PRODUCTION", "OUT_SALE", "OUT_ADJUSTMENT", "IN_ADJUSTMENT", "OUT_LOSS":
		return true
	default:
		return false
	}
}

func SignedQuantity(movementType string, quantity decimal.Decimal) decimal.Decimal {
	if strings.HasPrefix(movementType, "OUT_") {
		return quantity.Neg()
	}
	return quantity
}

func MovementParams(req StockMovementRequest, userID uuid.UUID) (db.CreateStockMovementParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, err
	}
	variantID, err := v.ParseUUID(req.VariantID, "variant_id")
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, err
	}
	quantity, err := decimal.NewFromString(req.Quantity)
	if err != nil || !quantity.IsPositive() {
		return db.CreateStockMovementParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "quantity must be positive")
	}
	unitCost, err := api.NullDecimal(req.UnitCost)
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "unit_cost must be a decimal")
	}
	movementType := strings.ToUpper(v.Clean(req.MovementType))
	if !ValidMovementType(movementType) {
		return db.CreateStockMovementParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "movement_type is invalid")
	}
	return db.CreateStockMovementParams{
		BusinessID:    businessID,
		VariantID:     variantID,
		MovementType:  movementType,
		Quantity:      quantity,
		UnitCost:      unitCost,
		ReferenceType: api.NullString(v.CleanOptional(req.ReferenceType)),
		ReferenceID:   api.NullString(v.CleanOptional(req.ReferenceID)),
		Notes:         v.Clean(req.Notes),
		CreatedBy:     uuid.NullUUID{UUID: userID, Valid: true},
	}, businessID, nil
}
