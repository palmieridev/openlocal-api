package marketplacepostgres

import (
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/marketplace/discovery"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"strings"
)

func mapProducts(rows []db.SearchMarketplaceProductsRow) []discovery.Product {
	out := make([]discovery.Product, 0, len(rows))
	for _, row := range rows {
		out = append(out, discovery.Product{
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
