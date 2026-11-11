// Package reporting owns private inventory analytics and EOQ assumptions.
package reporting

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ErrNotFound = errors.New("not found")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{message} }

// Identity has already been authorized as owner or manager by the caller boundary.
type Identity struct{ BusinessID, ActorID uuid.UUID }
type Page struct{ Limit, Offset int32 }
type ABC struct {
	ProductID                          uuid.UUID
	Name                               string
	Value, TotalValue, CumulativeValue decimal.Decimal
	Class                              string
}
type LowStock struct {
	VariantID, ProductID         uuid.UUID
	SKU, Name                    string
	QuantityOnHand, ReorderPoint decimal.Decimal
}
type EOQInput struct {
	VariantID                 uuid.UUID
	PeriodDays                int32
	OrderCost, HoldingPercent string
}
type EOQ struct {
	VariantID                      uuid.UUID
	PeriodDays                     int32
	Demand, OrderCost, HoldingRate decimal.Decimal
	Estimated                      float64
}
type Store interface {
	ABC(context.Context, uuid.UUID, Page) ([]ABC, error)
	LowStock(context.Context, uuid.UUID, Page) ([]LowStock, error)
	RequireVariant(context.Context, uuid.UUID, uuid.UUID) error
	Demand(context.Context, uuid.UUID, uuid.UUID, int32) (decimal.Decimal, error)
}
type Service struct{ store Store }

func New(store Store) *Service { return &Service{store} }
func identity(id Identity) error {
	if id.BusinessID == uuid.Nil || id.ActorID == uuid.Nil {
		return invalid("authorized business and actor are required")
	}
	return nil
}
func page(p Page) (Page, error) {
	if p.Limit == 0 {
		p.Limit = 25
	}
	if p.Limit < 1 || p.Limit > 100 {
		return p, invalid("limit must be between 1 and 100")
	}
	if p.Offset < 0 || p.Offset > 2000000000 {
		return p, invalid("offset must be between 0 and 2000000000")
	}
	return p, nil
}
func (s *Service) ABC(ctx context.Context, id Identity, p Page) ([]ABC, error) {
	if err := identity(id); err != nil {
		return nil, err
	}
	p, err := page(p)
	if err != nil {
		return nil, err
	}
	return s.store.ABC(ctx, id.BusinessID, p)
}
func (s *Service) LowStock(ctx context.Context, id Identity, p Page) ([]LowStock, error) {
	if err := identity(id); err != nil {
		return nil, err
	}
	p, err := page(p)
	if err != nil {
		return nil, err
	}
	return s.store.LowStock(ctx, id.BusinessID, p)
}
func cost(value, field, fallback string, max decimal.Decimal, scale int32) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if len(value) > 64 {
		return decimal.Zero, invalid(field + " must be a decimal with at most 64 characters")
	}
	d, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, invalid(field + " must be a decimal")
	}
	if d.IsNegative() || d.GreaterThan(max) {
		return decimal.Zero, invalid(fmt.Sprintf("%s must be between 0 and %s", field, max))
	}
	if d.Exponent() < -scale {
		return decimal.Zero, invalid(fmt.Sprintf("%s must have at most %d decimal places", field, scale))
	}
	return d, nil
}
func (s *Service) EOQ(ctx context.Context, id Identity, in EOQInput) (EOQ, error) {
	if err := identity(id); err != nil {
		return EOQ{}, err
	}
	if in.VariantID == uuid.Nil {
		return EOQ{}, invalid("variant_id must be a valid uuid")
	}
	if in.PeriodDays == 0 {
		in.PeriodDays = 90
	}
	if in.PeriodDays < 1 || in.PeriodDays > 730 {
		return EOQ{}, invalid("period_days must be between 1 and 730")
	}
	order, err := cost(in.OrderCost, "estimated_order_cost", "100", decimal.RequireFromString("9999999999.99"), 2)
	if err != nil {
		return EOQ{}, err
	}
	holding, err := cost(in.HoldingPercent, "estimated_holding_cost_percent", "20", decimal.NewFromInt(1000), 4)
	if err != nil {
		return EOQ{}, err
	}
	if err = s.store.RequireVariant(ctx, id.BusinessID, in.VariantID); err != nil {
		return EOQ{}, err
	}
	demand, err := s.store.Demand(ctx, id.BusinessID, in.VariantID, in.PeriodDays)
	if err != nil {
		return EOQ{}, err
	}
	rate := holding.Div(decimal.NewFromInt(100))
	out := EOQ{VariantID: in.VariantID, PeriodDays: in.PeriodDays, Demand: demand, OrderCost: order, HoldingRate: rate}
	if demand.IsPositive() && order.IsPositive() && rate.IsPositive() {
		d, _ := demand.Float64()
		o, _ := order.Float64()
		h, _ := rate.Float64()
		out.Estimated = math.Sqrt(2 * d * o / h)
	}
	return out, nil
}
