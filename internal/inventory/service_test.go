package inventory

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func TestSignedQuantity(t *testing.T) {
	qty := decimal.NewFromInt(3)
	if got := SignedQuantity("IN_PURCHASE", qty); !got.Equal(qty) {
		t.Fatalf("expected positive in movement, got %s", got)
	}
	if got := SignedQuantity("OUT_SALE", qty); !got.Equal(qty.Neg()) {
		t.Fatalf("expected negative out movement, got %s", got)
	}
}

func TestMovementDelta(t *testing.T) {
	old := db.StockMovement{MovementType: "IN_PURCHASE", Quantity: decimal.NewFromInt(5)}
	tests := []struct {
		name     string
		newType  string
		quantity decimal.Decimal
		want     decimal.Decimal
	}{
		{name: "edit inbound up", newType: "IN_PURCHASE", quantity: decimal.NewFromInt(8), want: decimal.NewFromInt(3)},
		{name: "edit inbound down", newType: "IN_PURCHASE", quantity: decimal.NewFromInt(2), want: decimal.NewFromInt(-3)},
		{name: "change to outbound", newType: "OUT_ADJUSTMENT", quantity: decimal.NewFromInt(2), want: decimal.NewFromInt(-7)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MovementDelta(old, tt.newType, tt.quantity); !got.Equal(tt.want) {
				t.Fatalf("MovementDelta() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMovementParamsRejectsUnsafeValues(t *testing.T) {
	base := StockMovementRequest{
		BusinessID:   uuid.New().String(),
		VariantID:    uuid.New().String(),
		MovementType: "IN_PURCHASE",
		Quantity:     "1.000",
	}
	tests := []struct {
		name   string
		mutate func(*StockMovementRequest)
	}{
		{"quantity scale", func(req *StockMovementRequest) { req.Quantity = "1.0001" }},
		{"negative unit cost", func(req *StockMovementRequest) { value := "-1"; req.UnitCost = &value }},
		{"notes length", func(req *StockMovementRequest) { req.Notes = strings.Repeat("x", 2001) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := base
			test.mutate(&req)
			if _, _, err := MovementParams(req, uuid.New(), "movement:12345678"); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestStockMovementResponseExcludesPersistenceFields(t *testing.T) {
	movement := db.StockMovement{
		ID:             uuid.New(),
		BusinessID:     uuid.New(),
		VariantID:      uuid.New(),
		LocationID:     uuid.New(),
		MovementType:   "IN_PURCHASE",
		Quantity:       decimal.NewFromInt(1),
		CreatedBy:      uuid.NullUUID{UUID: uuid.New(), Valid: true},
		IdempotencyKey: sql.NullString{String: "movement:12345678", Valid: true},
	}
	body, err := json.Marshal(MapStockMovement(movement))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"business_id", "created_by", "idempotency_key"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("response exposed %s: %s", forbidden, body)
		}
	}
}
