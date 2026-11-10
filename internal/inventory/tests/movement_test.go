package inventory_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/shopspring/decimal"
)

func TestParseRules(t *testing.T) {
	base := movement.Input{BusinessID: uuid.New(), ActorID: uuid.New(), VariantID: uuid.NewString(), MovementType: " in_purchase ", Quantity: "1.000", Notes: "  two\nwords ", IdempotencyKey: "create:12345678"}
	got, err := movement.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.MovementType != "IN_PURCHASE" || got.Notes != "two words" || *got.ActorID != base.ActorID || got.BusinessID != base.BusinessID {
		t.Fatalf("normalization or identity lost: %+v", got)
	}
	for _, test := range []struct {
		name   string
		change func(*movement.Input)
	}{
		{"zero quantity", func(i *movement.Input) { i.Quantity = "0" }},
		{"excess scale", func(i *movement.Input) { i.Quantity = "1.0000" }},
		{"overflow", func(i *movement.Input) { i.Quantity = "1000000000" }},
		{"invalid type", func(i *movement.Input) { i.MovementType = "TRANSFER" }},
		{"negative cost", func(i *movement.Input) { v := "-0.01"; i.UnitCost = &v }},
		{"long notes", func(i *movement.Input) { i.Notes = strings.Repeat("é", 2001) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			in := base
			test.change(&in)
			_, err := movement.Parse(in)
			var validation *movement.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
	for _, kind := range []string{"IN_PURCHASE", "IN_PRODUCTION", "IN_ADJUSTMENT", "OUT_SALE", "OUT_ADJUSTMENT", "OUT_LOSS"} {
		qty := movement.SignedQuantity(kind, decimal.NewFromInt(2))
		if qty.IsNegative() != strings.HasPrefix(kind, "OUT_") {
			t.Fatalf("incorrect sign for %s", kind)
		}
	}
}

func TestReplayComparison(t *testing.T) {
	actor := uuid.New()
	cost := decimal.RequireFromString("1.00")
	ref := "invoice"
	base := movement.Data{BusinessID: uuid.New(), VariantID: uuid.New(), LocationID: uuid.New(), ActorID: &actor, MovementType: "IN_PURCHASE", Quantity: decimal.RequireFromString("2.000"), UnitCost: &cost, ReferenceType: &ref}
	same := base
	same.Quantity = decimal.NewFromInt(2)
	otherCost := decimal.NewFromInt(1)
	same.UnitCost = &otherCost
	if !movement.Same(base, same) {
		t.Fatal("decimal scale should not affect replay")
	}
	for _, test := range []struct {
		name   string
		change func(*movement.Data)
	}{
		{"actor", func(d *movement.Data) { v := uuid.New(); d.ActorID = &v }},
		{"deleted actor", func(d *movement.Data) { d.ActorID = nil }},
		{"business", func(d *movement.Data) { d.BusinessID = uuid.New() }},
		{"variant", func(d *movement.Data) { d.VariantID = uuid.New() }},
		{"location", func(d *movement.Data) { d.LocationID = uuid.New() }},
		{"type", func(d *movement.Data) { d.MovementType = "OUT_SALE" }},
		{"quantity", func(d *movement.Data) { d.Quantity = decimal.NewFromInt(3) }},
		{"absent cost", func(d *movement.Data) { d.UnitCost = nil }},
		{"reference", func(d *movement.Data) { d.ReferenceType = nil }},
		{"notes", func(d *movement.Data) { d.Notes = "changed" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			different := base
			test.change(&different)
			if movement.Same(base, different) {
				t.Fatal("different intent accepted as replay")
			}
		})
	}
}
