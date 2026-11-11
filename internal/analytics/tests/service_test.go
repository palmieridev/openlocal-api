package analytics_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/analytics/analyticspostgres"
	"github.com/palmieridev/openlocal-api/internal/analytics/reporting"
	"github.com/shopspring/decimal"
	"math"
	"testing"
)

type reportStore struct {
	reporting.Store
	business, variant uuid.UUID
	days              int32
	requires, reads   int
	err               error
	demand            decimal.Decimal
}

func (f *reportStore) RequireVariant(_ context.Context, business, variant uuid.UUID) error {
	f.requires++
	f.business = business
	f.variant = variant
	return f.err
}
func (f *reportStore) Demand(_ context.Context, business, variant uuid.UUID, days int32) (decimal.Decimal, error) {
	f.reads++
	if business != f.business || variant != f.variant {
		return decimal.Zero, errors.New("lost tenant scope")
	}
	f.days = days
	return f.demand, nil
}
func TestEOQServiceUsesVerifiedTenantAndManualAssumptions(t *testing.T) {
	store := &reportStore{demand: decimal.NewFromInt(40)}
	s := reporting.New(store)
	id := reporting.Identity{BusinessID: uuid.New(), ActorID: uuid.New()}
	variant := uuid.New()
	result, err := s.EOQ(context.Background(), id, reporting.EOQInput{VariantID: variant, OrderCost: "25", HoldingPercent: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(result.Estimated-math.Sqrt(20000)) > 0.000001 || store.business != id.BusinessID || store.variant != variant || store.days != 90 {
		t.Fatalf("result %+v store %+v", result, store)
	}
	store.err = reporting.ErrNotFound
	if _, err := s.EOQ(context.Background(), id, reporting.EOQInput{VariantID: uuid.New()}); !errors.Is(err, reporting.ErrNotFound) {
		t.Fatal(err)
	}
	if store.reads != 1 {
		t.Fatal("foreign variant reached demand query")
	}
}
func TestEOQServiceValidatesBeforeReads(t *testing.T) {
	store := &reportStore{}
	s := reporting.New(store)
	id := reporting.Identity{BusinessID: uuid.New(), ActorID: uuid.New()}
	variant := uuid.New()
	for _, in := range []reporting.EOQInput{{}, {VariantID: variant, PeriodDays: 731}, {VariantID: variant, OrderCost: "-1"}, {VariantID: variant, OrderCost: "1.001"}, {VariantID: variant, HoldingPercent: "1001"}, {VariantID: variant, HoldingPercent: "invalid"}} {
		_, err := s.EOQ(context.Background(), id, in)
		var invalid *reporting.ValidationError
		if !errors.As(err, &invalid) {
			t.Fatal("missing validation", err)
		}
	}
	if store.requires != 0 || store.reads != 0 {
		t.Fatal("invalid input reached persistence")
	}
	if _, err := s.ABC(context.Background(), reporting.Identity{}, reporting.Page{}); err == nil {
		t.Fatal("missing actor accepted")
	}
	if _, err := s.LowStock(context.Background(), id, reporting.Page{Limit: 101}); err == nil {
		t.Fatal("unbounded list accepted")
	}
}
func TestEOQPersistenceIndependentlyRejectsForeignVariants(t *testing.T) {
	f := setup(t)
	b := f.business(t, "draft")
	other := f.business(t, "draft")
	v := f.variant(t, other)
	store := analyticspostgres.New(f.q)
	for _, variant := range []uuid.UUID{v.ID, uuid.New()} {
		if _, err := store.Demand(context.Background(), b.ID, variant, 90); !errors.Is(err, reporting.ErrNotFound) {
			t.Fatalf("unscoped demand: %v", err)
		}
	}
}
