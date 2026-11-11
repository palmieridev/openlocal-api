package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/shopspring/decimal"
)

type changeFake struct {
	current                        movement.Movement
	fail                           string
	deltas                         []decimal.Decimal
	audit                          movement.DeleteInput
	action                         string
	updated                        movement.Movement
	deleted, committed, rolledBack bool
}

var changeFailure = errors.New("persistence failed")

func (f *changeFake) failure(operation string) error {
	if f.fail == operation {
		return changeFailure
	}
	return nil
}
func (f *changeFake) BeginChange(context.Context) (movement.ChangeTransaction, error) {
	return f, f.failure("begin")
}
func (f *changeFake) LockMovement(context.Context, uuid.UUID, uuid.UUID) (movement.Movement, error) {
	return f.current, f.failure("lock")
}
func (f *changeFake) RequireVariant(context.Context, uuid.UUID, uuid.UUID) error {
	return f.failure("variant")
}
func (f *changeFake) RequireLocation(context.Context, uuid.UUID, uuid.UUID) error {
	return f.failure("location")
}
func (f *changeFake) StockLevel(context.Context, movement.Data) (movement.StockLevel, error) {
	return movement.StockLevel{QuantityOnHand: decimal.NewFromInt(10)}, f.failure("level")
}
func (f *changeFake) LockStockLocations(context.Context, movement.Data, []uuid.UUID) error {
	return f.failure("stock locks")
}
func (f *changeFake) ApplyNonnegativeDelta(_ context.Context, _ movement.Data, d decimal.Decimal) (movement.StockLevel, error) {
	f.deltas = append(f.deltas, d)
	return movement.StockLevel{}, f.failure("delta")
}
func (f *changeFake) Update(_ context.Context, m movement.Movement) (movement.Movement, error) {
	f.updated = m
	return m, f.failure("update")
}
func (f *changeFake) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	f.deleted = true
	return f.failure("delete")
}
func (f *changeFake) AuditChange(_ context.Context, input movement.DeleteInput, action string) error {
	f.audit = input
	f.action = action
	return f.failure("audit")
}
func (f *changeFake) Commit(context.Context) error {
	if err := f.failure("commit"); err != nil {
		return err
	}
	f.committed = true
	return nil
}
func (f *changeFake) Rollback(context.Context) error { f.rolledBack = true; return nil }

func changeFixture() (*changeFake, movement.EditInput) {
	actor := uuid.New()
	data := movement.Data{BusinessID: uuid.New(), VariantID: uuid.New(), LocationID: uuid.New(), ActorID: &actor, MovementType: "IN_PURCHASE", Quantity: decimal.NewFromInt(5), IdempotencyKey: "original:12345678"}
	f := &changeFake{current: movement.Movement{ID: uuid.New(), Data: data}}
	input := movement.EditInput{MovementID: f.current.ID, Input: movement.Input{BusinessID: data.BusinessID, ActorID: uuid.New(), VariantID: data.VariantID.String(), MovementType: data.MovementType, Quantity: "8", IdempotencyKey: "edit:12345678"}}
	return f, input
}

func TestMovementEditSignedDeltas(t *testing.T) {
	for _, test := range []struct{ name, oldType, newType, quantity, want string }{
		{"inbound up", "IN_PURCHASE", "IN_PURCHASE", "8", "3"},
		{"inbound down", "IN_PURCHASE", "IN_PURCHASE", "2", "-3"},
		{"inbound to outbound", "IN_PURCHASE", "OUT_ADJUSTMENT", "2", "-7"},
		{"outbound to inbound", "OUT_LOSS", "IN_ADJUSTMENT", "2", "7"},
		{"outbound increase", "OUT_SALE", "OUT_SALE", "8", "-3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, input := changeFixture()
			f.current.MovementType = test.oldType
			input.MovementType = test.newType
			input.Quantity = test.quantity
			result, err := movement.NewMaintenance(f).Edit(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if len(f.deltas) != 1 || !f.deltas[0].Equal(decimal.RequireFromString(test.want)) {
				t.Fatalf("deltas=%v want %s", f.deltas, test.want)
			}
			if !f.committed || f.audit.ActorID != input.ActorID || f.updated.ActorID == nil || *f.updated.ActorID != *f.current.ActorID || result.Movement.CreatedAt != f.current.CreatedAt {
				t.Fatal("edit changed creator or failed to audit the editor")
			}
		})
	}
}

func TestMovementEditReplayIgnoresEditor(t *testing.T) {
	f, input := changeFixture()
	input.Quantity = "5.000"
	input.IdempotencyKey = f.current.IdempotencyKey
	result, err := movement.NewMaintenance(f).Edit(context.Background(), input)
	if err != nil || !result.Replayed || len(f.deltas) != 0 || f.action != "" || !f.committed {
		t.Fatalf("replay=%+v err=%v writes=%v", result, err, f.deltas)
	}
	f, input = changeFixture()
	input.IdempotencyKey = f.current.IdempotencyKey
	if _, err := movement.NewMaintenance(f).Edit(context.Background(), input); !errors.Is(err, movement.ErrEditConflict) {
		t.Fatalf("changed same-key payload: %v", err)
	}
	if len(f.deltas) != 0 || f.committed {
		t.Fatal("conflicting replay wrote state")
	}
}

func TestMovementEditFailuresDoNotCommit(t *testing.T) {
	for _, operation := range []string{"begin", "lock", "variant", "location", "delta", "update", "audit", "commit"} {
		t.Run(operation, func(t *testing.T) {
			f, input := changeFixture()
			f.fail = operation
			if _, err := movement.NewMaintenance(f).Edit(context.Background(), input); !errors.Is(err, changeFailure) {
				t.Fatalf("failure lost: %v", err)
			}
			if f.committed || operation != "begin" && !f.rolledBack {
				t.Fatal("failed edit did not abort")
			}
			if (operation == "variant" || operation == "location") && len(f.deltas) != 0 {
				t.Fatal("ownership rejection changed stock")
			}
		})
	}
}

func TestMovementEditRejectsVariantChangeBeforeWrites(t *testing.T) {
	f, input := changeFixture()
	input.VariantID = uuid.NewString()
	_, err := movement.NewMaintenance(f).Edit(context.Background(), input)
	var validation *movement.ValidationError
	if !errors.As(err, &validation) || len(f.deltas) != 0 || f.committed {
		t.Fatalf("immutable variant: %v", err)
	}
}

func TestMovementDeleteUsesInverseSignAndAbortsFailures(t *testing.T) {
	for _, kind := range []string{"IN_PURCHASE", "OUT_SALE"} {
		t.Run(kind, func(t *testing.T) {
			f, input := changeFixture()
			f.current.MovementType = kind
			err := movement.NewMaintenance(f).Delete(context.Background(), movement.DeleteInput{BusinessID: input.BusinessID, ActorID: input.ActorID, MovementID: input.MovementID})
			want := decimal.NewFromInt(-5)
			if kind == "OUT_SALE" {
				want = want.Neg()
			}
			if err != nil || len(f.deltas) != 1 || !f.deltas[0].Equal(want) || !f.deleted || !f.committed || f.action != "inventory.movement.delete" {
				t.Fatalf("delete sign or effects: %+v, %v", f, err)
			}
		})
	}
	for _, operation := range []string{"lock", "variant", "location", "delta", "delete", "audit", "commit"} {
		t.Run(operation, func(t *testing.T) {
			f, input := changeFixture()
			f.fail = operation
			err := movement.NewMaintenance(f).Delete(context.Background(), movement.DeleteInput{BusinessID: input.BusinessID, ActorID: input.ActorID, MovementID: input.MovementID})
			if !errors.Is(err, changeFailure) || f.committed || !f.rolledBack {
				t.Fatalf("delete failure: %v", err)
			}
		})
	}
}

type listFake struct {
	received movement.ListInput
	calls    int
}

func (f *listFake) ListMovements(_ context.Context, input movement.ListInput) ([]movement.Movement, error) {
	f.received = input
	f.calls++
	return []movement.Movement{}, nil
}
func (f *listFake) ListLevels(_ context.Context, input movement.ListInput) ([]movement.StockLevel, error) {
	f.received = input
	f.calls++
	return []movement.StockLevel{}, nil
}
func TestInventoryReaderBoundsAndBusinessScope(t *testing.T) {
	for _, levels := range []bool{false, true} {
		f := &listFake{}
		service := movement.NewReader(f)
		list := func(input movement.ListInput) error {
			if levels {
				_, err := service.Levels(context.Background(), input)
				return err
			}
			_, err := service.Movements(context.Background(), input)
			return err
		}
		input := movement.ListInput{BusinessID: uuid.New()}
		if err := list(input); err != nil {
			t.Fatal(err)
		}
		if f.received.BusinessID != input.BusinessID || f.received.Limit != 25 || f.received.Offset != 0 {
			t.Fatalf("lost defaults or tenant: %+v", f.received)
		}
		for _, bad := range []movement.ListInput{{Limit: -1}, {Limit: 101}, {Limit: 25, Offset: -1}, {Limit: 25, Offset: 2_000_000_001}} {
			var validation *movement.ValidationError
			if err := list(bad); !errors.As(err, &validation) {
				t.Fatalf("invalid page: %v", err)
			}
		}
		if f.calls != 1 {
			t.Fatal("invalid request reached persistence")
		}
	}
}
