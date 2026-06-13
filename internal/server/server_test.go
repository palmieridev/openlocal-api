package server

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func TestPublicVariantDTOExcludesPrivateFields(t *testing.T) {
	variant := db.ProductVariant{
		ID:                uuid.New(),
		ProductID:         uuid.New(),
		BusinessID:        uuid.New(),
		Sku:               "SKU-1",
		InternalCode:      "PRIVATE-CODE",
		Name:              "Small",
		Price:             decimal.NewFromInt(100),
		Cost:              decimal.NullDecimal{Decimal: decimal.NewFromInt(20), Valid: true},
		Currency:          "MXN",
		TrackInventory:    true,
		PublicStockStatus: "available",
		ReorderPoint:      decimal.NewFromInt(5),
		Status:            "active",
	}
	body, err := json.Marshal(mapVariant(variant, false))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"cost", "internal_code", "business_id", "track_inventory", "reorder_point"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("public DTO exposed %s: %s", forbidden, string(body))
		}
	}
}

func TestSignedQuantity(t *testing.T) {
	qty := decimal.NewFromInt(3)
	if got := signedQuantity("IN_PURCHASE", qty); !got.Equal(qty) {
		t.Fatalf("expected positive in movement, got %s", got)
	}
	if got := signedQuantity("OUT_SALE", qty); !got.Equal(qty.Neg()) {
		t.Fatalf("expected negative out movement, got %s", got)
	}
}
