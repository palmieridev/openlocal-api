package marketplace

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func TestMapProductsIncludesVariantDetails(t *testing.T) {
	rows := []db.SearchMarketplaceProductsRow{{
		BusinessSlug:       "carpinteria-a-domicilio",
		BusinessName:       "Carpintería a Domicilio",
		ID:                 uuid.New(),
		Name:               "Clóset",
		Slug:               "closet",
		Description:        "Muebles a medida",
		Unit:               "proyecto",
		ProductType:        "made_to_order_product",
		VariantID:          uuid.New(),
		Sku:                "CLOSET-01",
		VariantName:        "Clóset pequeño",
		VariantDescription: sql.NullString{String: "Melamina gris con seis puertas", Valid: true},
		PriceNote:          sql.NullString{String: "Precio sujeto a medidas y acabados", Valid: true},
		Price:              decimal.NewFromInt(5000),
		Currency:           "MXN",
		PublicStockStatus:  "made_to_order",
	}}

	products := MapProducts(rows)
	if len(products) != 1 {
		t.Fatalf("len = %d, want 1", len(products))
	}
	if products[0].VariantDescription == nil || *products[0].VariantDescription != rows[0].VariantDescription.String {
		t.Fatalf("variant_description = %v", products[0].VariantDescription)
	}
	if products[0].PriceNote == nil || *products[0].PriceNote != rows[0].PriceNote.String {
		t.Fatalf("price_note = %v", products[0].PriceNote)
	}
}
