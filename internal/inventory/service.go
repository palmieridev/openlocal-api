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

func MovementParams(req StockMovementRequest, userID uuid.UUID, idempotencyKey string) (db.CreateStockMovementParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, err
	}
	variantID, err := v.ParseUUID(req.VariantID, "variant_id")
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, err
	}
	quantity, err := v.ParseDecimal(req.Quantity, "quantity")
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, err
	}
	if err := v.DecimalRange(quantity, "quantity", decimal.RequireFromString("0.001"), decimal.RequireFromString("999999999.999"), 3); err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, err
	}
	unitCost, err := api.NullDecimal(req.UnitCost)
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "unit_cost must be a decimal")
	}
	if unitCost.Valid {
		if err := v.DecimalRange(unitCost.Decimal, "unit_cost", decimal.Zero, decimal.RequireFromString("9999999999.99"), 2); err != nil {
			return db.CreateStockMovementParams{}, uuid.Nil, err
		}
	}
	movementType := strings.ToUpper(v.Clean(req.MovementType))
	if !ValidMovementType(movementType) {
		return db.CreateStockMovementParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "movement_type is invalid")
	}
	referenceType := v.CleanOptional(req.ReferenceType)
	referenceID := v.CleanOptional(req.ReferenceID)
	if referenceType != nil {
		if err := v.StringLength(*referenceType, "reference_type", 1, 80); err != nil {
			return db.CreateStockMovementParams{}, uuid.Nil, err
		}
	}
	if referenceID != nil {
		if err := v.StringLength(*referenceID, "reference_id", 1, 160); err != nil {
			return db.CreateStockMovementParams{}, uuid.Nil, err
		}
	}
	notes := v.Clean(req.Notes)
	if err := v.StringLength(notes, "notes", 0, 2000); err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, err
	}
	return db.CreateStockMovementParams{
		BusinessID:     businessID,
		VariantID:      variantID,
		MovementType:   movementType,
		Quantity:       quantity,
		UnitCost:       unitCost,
		ReferenceType:  api.NullString(referenceType),
		ReferenceID:    api.NullString(referenceID),
		Notes:          notes,
		CreatedBy:      uuid.NullUUID{UUID: userID, Valid: true},
		IdempotencyKey: api.NullString(&idempotencyKey),
	}, businessID, nil
}

func SameMovement(movement db.StockMovement, params db.CreateStockMovementParams) bool {
	return movement.BusinessID == params.BusinessID &&
		movement.VariantID == params.VariantID &&
		movement.LocationID == params.LocationID &&
		movement.MovementType == params.MovementType &&
		movement.Quantity.Equal(params.Quantity) &&
		sameNullDecimal(movement.UnitCost, params.UnitCost) &&
		movement.ReferenceType == params.ReferenceType &&
		movement.ReferenceID == params.ReferenceID &&
		movement.Notes == params.Notes &&
		movement.CreatedBy == params.CreatedBy
}

func sameNullDecimal(left, right decimal.NullDecimal) bool {
	if left.Valid != right.Valid {
		return false
	}
	return !left.Valid || left.Decimal.Equal(right.Decimal)
}
