package analytics

import (
	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type ABCResponse struct {
	ProductID       uuid.UUID `json:"product_id"`
	Name            string    `json:"name"`
	Value           string    `json:"value"`
	TotalValue      string    `json:"total_value"`
	CumulativeValue string    `json:"cumulative_value"`
	Class           string    `json:"class"`
}

type LowStockResponse struct {
	VariantID      uuid.UUID `json:"variant_id"`
	ProductID      uuid.UUID `json:"product_id"`
	SKU            string    `json:"sku"`
	Name           string    `json:"name"`
	QuantityOnHand string    `json:"quantity_on_hand"`
	ReorderPoint   string    `json:"reorder_point"`
}

func MapABC(rows []db.GetABCAnalysisRow) []ABCResponse {
	out := make([]ABCResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, ABCResponse{
			ProductID:       row.ProductID,
			Name:            row.Name,
			Value:           row.Value.String(),
			TotalValue:      row.TotalValue.String(),
			CumulativeValue: row.CumulativeValue.String(),
			Class:           row.Class,
		})
	}
	return out
}

func MapLowStock(rows []db.GetLowStockRow) []LowStockResponse {
	out := make([]LowStockResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, LowStockResponse{
			VariantID:      row.VariantID,
			ProductID:      row.ProductID,
			SKU:            row.Sku,
			Name:           row.Name,
			QuantityOnHand: row.QuantityOnHand.String(),
			ReorderPoint:   row.ReorderPoint.String(),
		})
	}
	return out
}
