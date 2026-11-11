package inventory_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func TestMovementMaintenanceAuditFailureRollsBack(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	q := db.New(pool)
	store := movementpostgres.New(pool, q)
	creator := movement.New(store)
	maintenance := movement.NewMaintenance(store)
	for _, operation := range []string{"edit", "delete"} {
		t.Run(operation, func(t *testing.T) {
			input := createMovementInput(t, pool)
			created, err := creator.Create(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			// Match one fixture only; unrelated packages may write audits concurrently.
			_, err = pool.Exec(ctx, `CREATE FUNCTION inventory_test_reject_change_audit() RETURNS trigger LANGUAGE plpgsql AS $$
  BEGIN IF NEW.business_id='`+input.BusinessID.String()+`'::uuid AND NEW.action IN ('inventory.movement.update','inventory.movement.delete') THEN
  RAISE EXCEPTION 'inventory change audit failure'; END IF; RETURN NEW; END $$;
  CREATE TRIGGER inventory_test_reject_change_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION inventory_test_reject_change_audit()`)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := pool.Exec(ctx, `DROP TRIGGER inventory_test_reject_change_audit ON audit_logs; DROP FUNCTION inventory_test_reject_change_audit()`); err != nil {
					t.Error(err)
				}
			}()
			changed := input
			changed.Quantity = "8"
			changed.IdempotencyKey = "retry:" + uuid.NewString()
			// Use a second stock row for edits to prove BOTH sides of a transfer roll back.
			destination, err := q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: input.BusinessID, Name: "Rollback destination"})
			if err != nil {
				t.Fatal(err)
			}
			dest := destination.ID.String()
			changed.LocationID = &dest
			if operation == "edit" {
				_, err = maintenance.Edit(ctx, movement.EditInput{Input: changed, MovementID: created.Movement.ID})
			} else {
				err = maintenance.Delete(ctx, movement.DeleteInput{BusinessID: input.BusinessID, ActorID: input.ActorID, MovementID: created.Movement.ID})
			}
			if err == nil || !strings.Contains(err.Error(), "inventory change audit failure") {
				t.Fatalf("expected injected audit failure: %v", err)
			}
			stored, err := q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{ID: created.Movement.ID, BusinessID: input.BusinessID})
			if err != nil {
				t.Fatal(err)
			}
			if !stored.Quantity.Equal(decimal.NewFromInt(5)) || stored.LocationID != created.Movement.LocationID || stored.IdempotencyKey.String != input.IdempotencyKey {
				t.Fatalf("movement changed after rollback: %+v", stored)
			}
			level, err := q.GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: created.Movement.VariantID, LocationID: created.Movement.LocationID})
			if err != nil || !level.QuantityOnHand.Equal(decimal.NewFromInt(5)) {
				t.Fatalf("stock changed after rollback: %+v %v", level, err)
			}
			assertMovementCounts(t, pool, input.BusinessID, 1, 1, 2, 1)
			// Remove this fixture's failure condition and verify the rolled-back key or
			// delete can be retried. The trigger remains fixture scoped until cleanup.
			if _, err := pool.Exec(ctx, `DROP TRIGGER inventory_test_reject_change_audit ON audit_logs; CREATE TRIGGER inventory_test_reject_change_audit BEFORE INSERT ON audit_logs FOR EACH ROW WHEN (false) EXECUTE FUNCTION inventory_test_reject_change_audit()`); err != nil {
				t.Fatal(err)
			}
			if operation == "edit" {
				_, err = maintenance.Edit(ctx, movement.EditInput{Input: changed, MovementID: created.Movement.ID})
			} else {
				err = maintenance.Delete(ctx, movement.DeleteInput{BusinessID: input.BusinessID, ActorID: input.ActorID, MovementID: created.Movement.ID})
			}
			if err != nil {
				t.Fatalf("retry after rollback: %v", err)
			}
		})
	}
}

func TestMovementEditKeyCollisionRollsBackStock(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	q := db.New(pool)
	store := movementpostgres.New(pool, q)
	creator := movement.New(store)
	input := createMovementInput(t, pool)
	first, err := creator.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second := input
	second.IdempotencyKey = "occupied:" + uuid.NewString()
	if _, err := creator.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	changed := input
	changed.Quantity = "8"
	changed.IdempotencyKey = second.IdempotencyKey
	if _, err := movement.NewMaintenance(store).Edit(ctx, movement.EditInput{Input: changed, MovementID: first.Movement.ID}); !errors.Is(err, movement.ErrConflict) {
		t.Fatalf("key collision: %v", err)
	}
	stored, err := q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{ID: first.Movement.ID, BusinessID: input.BusinessID})
	if err != nil || !stored.Quantity.Equal(decimal.NewFromInt(5)) || stored.IdempotencyKey.String != input.IdempotencyKey {
		t.Fatalf("movement changed after key collision: %+v %v", stored, err)
	}
	level, err := q.GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: first.Movement.VariantID, LocationID: first.Movement.LocationID})
	if err != nil || !level.QuantityOnHand.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("stock changed after key collision: %+v %v", level, err)
	}
	assertMovementCounts(t, pool, input.BusinessID, 2, 1, 1, 2)
}

func TestMovementEditStockOverflowRollsBack(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	q := db.New(pool)
	store := movementpostgres.New(pool, q)
	creator := movement.New(store)
	input := createMovementInput(t, pool)
	first, err := creator.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	seed := input
	seed.IdempotencyKey = "capacity:" + uuid.NewString()
	seed.Quantity = "999999994.999"
	if _, err := creator.Create(ctx, seed); err != nil {
		t.Fatal(err)
	}
	changed := input
	changed.Quantity = "8"
	changed.IdempotencyKey = "overflow:" + uuid.NewString()
	if _, err := movement.NewMaintenance(store).Edit(ctx, movement.EditInput{Input: changed, MovementID: first.Movement.ID}); err == nil {
		t.Fatal("expected stock numeric overflow")
	}
	stored, err := q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{ID: first.Movement.ID, BusinessID: input.BusinessID})
	if err != nil || !stored.Quantity.Equal(decimal.NewFromInt(5)) || stored.IdempotencyKey.String != input.IdempotencyKey {
		t.Fatalf("movement changed after stock failure: %+v %v", stored, err)
	}
	level, err := q.GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: first.Movement.VariantID, LocationID: first.Movement.LocationID})
	if err != nil || level.QuantityOnHand.String() != "999999999.999" {
		t.Fatalf("stock changed after stock failure: %+v %v", level, err)
	}
	assertMovementCounts(t, pool, input.BusinessID, 2, 1, 1, 2)
}

func TestMovementNoopOnNegativeStockAndRecovery(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	q := db.New(pool)
	store := movementpostgres.New(pool, q)
	creator := movement.New(store)
	maintenance := movement.NewMaintenance(store)
	input := createMovementInput(t, pool)
	first, err := creator.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	outgoing := input
	outgoing.MovementType = "OUT_SALE"
	outgoing.Quantity = "10"
	outgoing.IdempotencyKey = "sale:" + uuid.NewString()
	negative, err := creator.Create(ctx, outgoing)
	if err != nil {
		t.Fatal(err)
	}
	edit := input
	edit.IdempotencyKey = "noop:" + uuid.NewString()
	noop, err := maintenance.Edit(ctx, movement.EditInput{Input: edit, MovementID: first.Movement.ID})
	if err != nil || noop.Replayed || noop.StockLevel.QuantityOnHand.String() != "-5" || !noop.StockLevel.UpdatedAt.Equal(negative.StockLevel.UpdatedAt) {
		t.Fatalf("no-op negative stock contract: %+v %v", noop, err)
	}
	edit.Quantity = "7"
	edit.IdempotencyKey = "insufficient:" + uuid.NewString()
	if _, err := maintenance.Edit(ctx, movement.EditInput{Input: edit, MovementID: first.Movement.ID}); !errors.Is(err, movement.ErrNegativeStock) {
		t.Fatalf("insufficient recovery accepted: %v", err)
	}
	assertMovementCounts(t, pool, input.BusinessID, 2, 1, 1, 3)
	edit.Quantity = "10"
	fixed, err := maintenance.Edit(ctx, movement.EditInput{Input: edit, MovementID: first.Movement.ID})
	if err != nil || !fixed.StockLevel.QuantityOnHand.IsZero() {
		t.Fatalf("stock recovery: %+v %v", fixed, err)
	}
	assertMovementCounts(t, pool, input.BusinessID, 2, 1, 1, 4)
}
