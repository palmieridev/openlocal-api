package analyticspostgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/analytics/reporting"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

type Store struct{ q *db.Queries }

func New(q *db.Queries) *Store { return &Store{q} }
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return reporting.ErrNotFound
	}
	return err
}
func (s *Store) RequireVariant(ctx context.Context, business, id uuid.UUID) error {
	_, err := s.q.GetVariantForBusiness(ctx, db.GetVariantForBusinessParams{ID: id, BusinessID: business})
	return translate(err)
}
func (s *Store) Demand(ctx context.Context, business, id uuid.UUID, days int32) (decimal.Decimal, error) {
	demand, err := s.q.GetDemandForEOQ(ctx, db.GetDemandForEOQParams{BusinessID: business, VariantID: id, Column3: days})
	return demand, translate(err)
}
func (s *Store) ABC(ctx context.Context, business uuid.UUID, page reporting.Page) ([]reporting.ABC, error) {
	rows, err := s.q.GetABCAnalysis(ctx, db.GetABCAnalysisParams{BusinessID: business, Limit: page.Limit, Offset: page.Offset})
	if err != nil {
		return nil, err
	}
	out := make([]reporting.ABC, 0, len(rows))
	for _, r := range rows {
		out = append(out, reporting.ABC{ProductID: r.ProductID, Name: r.Name, Value: r.Value, TotalValue: r.TotalValue, CumulativeValue: r.CumulativeValue, Class: r.Class})
	}
	return out, nil
}
func (s *Store) LowStock(ctx context.Context, business uuid.UUID, page reporting.Page) ([]reporting.LowStock, error) {
	rows, err := s.q.GetLowStock(ctx, db.GetLowStockParams{BusinessID: business, Limit: page.Limit, Offset: page.Offset})
	if err != nil {
		return nil, err
	}
	out := make([]reporting.LowStock, 0, len(rows))
	for _, r := range rows {
		out = append(out, reporting.LowStock{VariantID: r.VariantID, ProductID: r.ProductID, SKU: r.Sku, Name: r.Name, QuantityOnHand: r.QuantityOnHand, ReorderPoint: r.ReorderPoint})
	}
	return out, nil
}
