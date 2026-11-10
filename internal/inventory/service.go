package inventory

import (
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/shopspring/decimal"
)

func ValidMovementType(value string) bool { return movement.ValidMovementType(value) }
func SignedQuantity(kind string, quantity decimal.Decimal) decimal.Decimal {
	return movement.SignedQuantity(kind, quantity)
}

func MovementParams(req StockMovementRequest, userID uuid.UUID, key string) (db.CreateStockMovementParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, err
	}
	data, err := movement.Parse(movementInput(req, businessID, userID, key))
	if err != nil {
		return db.CreateStockMovementParams{}, uuid.Nil, movementHTTPError(err)
	}
	return movementpostgres.Params(data), businessID, nil
}

func SameMovementEdit(movement db.StockMovement, params db.UpdateStockMovementParams) bool {
	return movement.BusinessID == params.BusinessID &&
		movement.LocationID == params.LocationID &&
		movement.MovementType == params.MovementType &&
		movement.Quantity.Equal(params.Quantity) &&
		sameNullDecimal(movement.UnitCost, params.UnitCost) &&
		movement.ReferenceType == params.ReferenceType &&
		movement.ReferenceID == params.ReferenceID &&
		movement.Notes == params.Notes
}

func MovementDelta(oldMovement db.StockMovement, newType string, newQuantity decimal.Decimal) decimal.Decimal {
	return SignedQuantity(newType, newQuantity).Sub(SignedQuantity(oldMovement.MovementType, oldMovement.Quantity))
}

func sameNullDecimal(left, right decimal.NullDecimal) bool {
	if left.Valid != right.Valid {
		return false
	}
	return !left.Valid || left.Decimal.Equal(right.Decimal)
}
