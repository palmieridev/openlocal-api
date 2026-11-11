package analytics

import (
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/analytics/reporting"
	"strconv"
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

func abcResponses(rows []reporting.ABC) []ABCResponse {
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

func lowStockResponses(rows []reporting.LowStock) []LowStockResponse {
	out := make([]LowStockResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, LowStockResponse{
			VariantID:      row.VariantID,
			ProductID:      row.ProductID,
			SKU:            row.SKU,
			Name:           row.Name,
			QuantityOnHand: row.QuantityOnHand.String(),
			ReorderPoint:   row.ReorderPoint.String(),
		})
	}
	return out
}

type EOQResponse struct {
	VariantID       uuid.UUID `json:"variant_id"`
	PeriodDays      int32     `json:"period_days"`
	Demand          string    `json:"demand"`
	EstimatedEOQ    string    `json:"estimated_eoq"`
	OrderCost       string    `json:"order_cost"`
	HoldingCostRate string    `json:"holding_cost_rate"`
}

func eoqResponse(r reporting.EOQ) EOQResponse {
	return EOQResponse{VariantID: r.VariantID, PeriodDays: r.PeriodDays, Demand: r.Demand.String(), EstimatedEOQ: strconv.FormatFloat(r.Estimated, 'f', 3, 64), OrderCost: r.OrderCost.String(), HoldingCostRate: r.HoldingRate.String()}
}
