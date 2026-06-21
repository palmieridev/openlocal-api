package inventory

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestSignedQuantity(t *testing.T) {
	qty := decimal.NewFromInt(3)
	if got := SignedQuantity("IN_PURCHASE", qty); !got.Equal(qty) {
		t.Fatalf("expected positive in movement, got %s", got)
	}
	if got := SignedQuantity("OUT_SALE", qty); !got.Equal(qty.Neg()) {
		t.Fatalf("expected negative out movement, got %s", got)
	}
}
