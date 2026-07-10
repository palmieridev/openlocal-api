package inventory

import (
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type StockMovementRequest struct {
	BusinessID    string  `json:"business_id"`
	VariantID     string  `json:"variant_id"`
	LocationID    *string `json:"location_id"`
	MovementType  string  `json:"movement_type"`
	Quantity      string  `json:"quantity"`
	UnitCost      *string `json:"unit_cost"`
	ReferenceType *string `json:"reference_type"`
	ReferenceID   *string `json:"reference_id"`
	Notes         string  `json:"notes"`
}

type StockMovementResponse struct {
	ID            uuid.UUID `json:"id"`
	VariantID     uuid.UUID `json:"variant_id"`
	LocationID    uuid.UUID `json:"location_id"`
	MovementType  string    `json:"movement_type"`
	Quantity      string    `json:"quantity"`
	UnitCost      *string   `json:"unit_cost,omitempty"`
	ReferenceType *string   `json:"reference_type,omitempty"`
	ReferenceID   *string   `json:"reference_id,omitempty"`
	Notes         string    `json:"notes"`
	CreatedAt     time.Time `json:"created_at"`
}

type StockLevelResponse struct {
	VariantID        uuid.UUID `json:"variant_id"`
	LocationID       uuid.UUID `json:"location_id"`
	QuantityOnHand   string    `json:"quantity_on_hand"`
	QuantityReserved string    `json:"quantity_reserved"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type MovementCreatedResponse struct {
	Movement   StockMovementResponse `json:"movement"`
	StockLevel StockLevelResponse    `json:"stock_level"`
}

func MapStockMovement(movement db.StockMovement) StockMovementResponse {
	return StockMovementResponse{
		ID:            movement.ID,
		VariantID:     movement.VariantID,
		LocationID:    movement.LocationID,
		MovementType:  movement.MovementType,
		Quantity:      movement.Quantity.String(),
		UnitCost:      api.DecimalPtr(movement.UnitCost),
		ReferenceType: api.StringPtr(movement.ReferenceType),
		ReferenceID:   api.StringPtr(movement.ReferenceID),
		Notes:         movement.Notes,
		CreatedAt:     api.TS(movement.CreatedAt),
	}
}

func MapStockLevel(level db.StockLevel) StockLevelResponse {
	return StockLevelResponse{
		VariantID:        level.VariantID,
		LocationID:       level.LocationID,
		QuantityOnHand:   level.QuantityOnHand.String(),
		QuantityReserved: level.QuantityReserved.String(),
		UpdatedAt:        api.TS(level.UpdatedAt),
	}
}
