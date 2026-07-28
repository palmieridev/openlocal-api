package catalog

import (
	"encoding/json"
	"strings"
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
	body, err := json.Marshal(MapVariant(variant, false))
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

func TestProductParamsRejectsInvalidEnumsAndLengths(t *testing.T) {
	businessID := uuid.New().String()
	for _, req := range []ProductRequest{
		{BusinessID: businessID, Name: "Coffee", ProductType: "subscription"},
		{BusinessID: businessID, Name: "Coffee", Status: "deleted"},
		{BusinessID: businessID, Name: strings.Repeat("x", 181)},
	} {
		if _, _, err := ProductParams(req); err == nil {
			t.Fatalf("expected validation error for %+v", req)
		}
	}
}

func TestMapProductListRowMapsOptionalImageURL(t *testing.T) {
	row := db.ListProductsRow{ImageUrl: " https://cdn.example.com/product.jpg "}
	product := MapProductListRow(row)
	if product.ImageURL == nil || *product.ImageURL != "https://cdn.example.com/product.jpg" {
		t.Fatalf("image_url = %v, want trimmed URL", product.ImageURL)
	}

	row.ImageUrl = ""
	if imageURL := MapProductListRow(row).ImageURL; imageURL != nil {
		t.Fatalf("image_url = %q, want nil", *imageURL)
	}
}

func TestVariantParamsRejectsUnsafeNumericAndEnumValues(t *testing.T) {
	base := VariantRequest{
		BusinessID: uuid.New().String(),
		ProductID:  uuid.New().String(),
		SKU:        "SKU-1",
		Price:      "10.00",
	}
	tests := []struct {
		name   string
		mutate func(*VariantRequest)
	}{
		{"price scale", func(req *VariantRequest) { req.Price = "10.001" }},
		{"negative cost", func(req *VariantRequest) { value := "-1"; req.Cost = &value }},
		{"currency", func(req *VariantRequest) { req.Currency = "MX" }},
		{"stock status", func(req *VariantRequest) { req.PublicStockStatus = "secret" }},
		{"lead time", func(req *VariantRequest) { req.LeadTimeDays = 3651 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := base
			test.mutate(&req)
			if _, _, err := VariantParams(req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestVariantParamsDefaultsAttributesToObject(t *testing.T) {
	req := VariantRequest{
		BusinessID: uuid.New().String(),
		ProductID:  uuid.New().String(),
		SKU:        "SKU-1",
		Price:      "10.00",
	}
	params, _, err := VariantParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if string(params.Attributes) != "{}" {
		t.Fatalf("attributes = %s, want {}", params.Attributes)
	}
}
