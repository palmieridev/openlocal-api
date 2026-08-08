package marketplace

import (
	"strings"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type ProductResponse struct {
	BusinessSlug       string    `json:"business_slug"`
	BusinessName       string    `json:"business_name"`
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	Slug               string    `json:"slug"`
	Description        string    `json:"description"`
	Brand              *string   `json:"brand,omitempty"`
	Unit               string    `json:"unit"`
	ProductType        string    `json:"product_type"`
	VariantID          uuid.UUID `json:"variant_id"`
	SKU                string    `json:"sku"`
	VariantName        string    `json:"variant_name"`
	VariantDescription *string   `json:"variant_description,omitempty"`
	PriceNote          *string   `json:"price_note,omitempty"`
	Price              string    `json:"price"`
	Currency           string    `json:"currency"`
	PublicStockStatus  string    `json:"public_stock_status"`
	ImageURL           *string   `json:"image_url,omitempty"`
}

func MapProducts(rows []db.SearchMarketplaceProductsRow) []ProductResponse {
	out := make([]ProductResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProductResponse{
			BusinessSlug:       row.BusinessSlug,
			BusinessName:       row.BusinessName,
			ID:                 row.ID,
			Name:               row.Name,
			Slug:               row.Slug,
			Description:        row.Description,
			Brand:              api.StringPtr(row.Brand),
			Unit:               row.Unit,
			ProductType:        row.ProductType,
			VariantID:          row.VariantID,
			SKU:                row.Sku,
			VariantName:        row.VariantName,
			VariantDescription: api.StringPtr(row.VariantDescription),
			PriceNote:          api.StringPtr(row.PriceNote),
			Price:              row.Price.StringFixed(2),
			Currency:           row.Currency,
			PublicStockStatus:  row.PublicStockStatus,
			ImageURL:           optionalString(row.ImageUrl),
		})
	}
	return out
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
