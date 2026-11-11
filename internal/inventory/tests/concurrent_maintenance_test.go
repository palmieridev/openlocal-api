package inventory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

type changeOutcome struct {
	result movement.Result
	err    error
}
type concurrentChange func(context.Context, *movement.Maintenance) changeOutcome

// The blocker holds a fixture row (or advisory lock used by a fixture trigger).
// Observe both workers blocked in PostgreSQL before releasing it. Each test has
// its own application_name, so concurrent package activity cannot meet the barrier.
func concurrentChanges(t *testing.T, pool *pgxpool.Pool, lock func(context.Context, pgx.Tx) error, operations ...concurrentChange) []changeOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	config := pool.Config()
	name := "inventory_overlap_" + uuid.NewString()
	config.ConnConfig.RuntimeParams["application_name"] = name
	workers, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer workers.Close()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	defer func() { _ = blocker.Rollback(context.Background()); cancel(); wg.Wait() }()
	if err := lock(ctx, blocker); err != nil {
		t.Fatal(err)
	}
	results := make([]changeOutcome, len(operations))
	service := movement.NewMaintenance(movementpostgres.New(workers, db.New(workers)))
	for index, op := range operations {
		wg.Add(1)
		go func() { defer wg.Done(); results[index] = op(ctx, service) }()
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND wait_event_type='Lock'`, name).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked == len(operations) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("workers did not reach the conflicting database operations")
		case <-ticker.C:
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	return results
}
func editOperation(input movement.Input, id uuid.UUID) concurrentChange {
	return func(ctx context.Context, s *movement.Maintenance) changeOutcome {
		r, err := s.Edit(ctx, movement.EditInput{Input: input, MovementID: id})
		return changeOutcome{r, err}
	}
}
func deleteOperation(input movement.Input, id uuid.UUID) concurrentChange {
	return func(ctx context.Context, s *movement.Maintenance) changeOutcome {
		return changeOutcome{err: s.Delete(ctx, movement.DeleteInput{BusinessID: input.BusinessID, ActorID: input.ActorID, MovementID: id})}
	}
}

func TestConcurrentMovementEditsAndDeletes(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	q := db.New(pool)
	creator := movement.New(movementpostgres.New(pool, q))
	for _, scenario := range []string{"edit edit", "same edit retry", "edit delete", "distinct edit edit", "distinct edit delete", "distinct competing deletes"} {
		t.Run(scenario, func(t *testing.T) {
			input := createMovementInput(t, pool)
			first, err := creator.Create(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			next := input
			next.Quantity = "8"
			next.IdempotencyKey = "edit:" + uuid.NewString()
			second := next
			second.Quantity = "9"
			second.IdempotencyKey = "other:" + uuid.NewString()
			id2 := first.Movement.ID
			distinct := scenario == "distinct edit edit" || scenario == "distinct edit delete" || scenario == "distinct competing deletes"
			if distinct {
				seed := input
				seed.IdempotencyKey = "seed:" + uuid.NewString()
				created, err := creator.Create(ctx, seed)
				if err != nil {
					t.Fatal(err)
				}
				id2 = created.Movement.ID
			}
			if scenario == "distinct competing deletes" {
				seed := input
				seed.IdempotencyKey = "consume:" + uuid.NewString()
				seed.MovementType = "OUT_SALE"
				seed.Quantity = "4"
				if _, err := creator.Create(ctx, seed); err != nil {
					t.Fatal(err)
				}
			}
			ops := []concurrentChange{editOperation(next, first.Movement.ID), editOperation(second, id2)}
			if scenario == "same edit retry" {
				ops[1] = editOperation(next, id2)
			}
			if scenario == "edit delete" || scenario == "distinct edit delete" {
				ops[1] = deleteOperation(input, id2)
			}
			if scenario == "distinct competing deletes" {
				ops[0] = deleteOperation(input, first.Movement.ID)
				ops[1] = deleteOperation(input, id2)
			}
			results := concurrentChanges(t, pool, func(ctx context.Context, tx pgx.Tx) error {
				if distinct {
					_, err := tx.Exec(ctx, "SELECT 1 FROM stock_levels WHERE business_id=$1 FOR UPDATE", input.BusinessID)
					return err
				}
				_, err := tx.Exec(ctx, "SELECT 1 FROM stock_movements WHERE id=$1 FOR UPDATE", first.Movement.ID)
				return err
			}, ops...)
			successes, replays, missing, negative := 0, 0, 0, 0
			for _, r := range results {
				switch {
				case r.err == nil:
					successes++
					if r.result.Replayed {
						replays++
					}
				case errors.Is(r.err, movement.ErrNotFound):
					missing++
				case errors.Is(r.err, movement.ErrNegativeStock):
					negative++
				default:
					t.Fatalf("concurrent operation: %v", r.err)
				}
			}
			movements, audits := 1, 3
			want := ""
			switch scenario {
			case "edit edit":
				if successes != 2 {
					t.Fatal(results)
				}
			case "same edit retry":
				if successes != 2 || replays != 1 {
					t.Fatal("retries did not apply once")
				}
				audits = 2
				want = "8"
			case "edit delete":
				if successes+missing != 2 || missing > 1 {
					t.Fatal(results)
				}
				movements = 0
				audits = successes + 1
				want = "0"
			case "distinct edit edit":
				if successes != 2 {
					t.Fatal(results)
				}
				movements = 2
				audits = 4
				want = "17"
			case "distinct edit delete":
				if successes != 2 {
					t.Fatal(results)
				}
				movements = 1
				audits = 4
				want = "8"
			case "distinct competing deletes":
				if successes != 1 || negative != 1 {
					t.Fatal("competing deletes crossed zero")
				}
				movements = 2
				audits = 4
				want = "1"
			}
			level, err := q.GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: first.Movement.VariantID, LocationID: first.Movement.LocationID})
			if err != nil {
				t.Fatal(err)
			}
			if want == "" {
				stored, err := q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{BusinessID: input.BusinessID, ID: first.Movement.ID})
				if err != nil {
					t.Fatal(err)
				}
				want = stored.Quantity.String()
				if want != "8" && want != "9" {
					t.Fatalf("unexpected final movement %s", want)
				}
			}
			if !level.QuantityOnHand.Equal(decimal.RequireFromString(want)) {
				t.Fatalf("stock=%s want=%s", level.QuantityOnHand, want)
			}
			assertMovementCounts(t, pool, input.BusinessID, movements, 1, 1, audits)
		})
	}
}

func TestConcurrentOppositeLocationTransfers(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	input := createMovementInput(t, pool)
	q := db.New(pool)
	creator := movement.New(movementpostgres.New(pool, q))
	first, err := creator.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	location, err := q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: input.BusinessID, Name: "Other warehouse"})
	if err != nil {
		t.Fatal(err)
	}
	id := location.ID.String()
	secondInput := input
	secondInput.LocationID = &id
	secondInput.IdempotencyKey = "second:" + uuid.NewString()
	second, err := creator.Create(ctx, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	// Pause after each source decrement while that source row remains locked.
	// Before the fix both transfers hold different source rows and then deadlock
	// on each other's destinations. With ordered prelocking one waits before
	// entering this trigger; both requests still reach an observed database wait.
	_, err = pool.Exec(ctx, `CREATE FUNCTION inventory_test_pause_transfer() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF NEW.business_id='`+input.BusinessID.String()+`'::uuid AND NEW.quantity_on_hand=0 THEN
 PERFORM pg_advisory_xact_lock_shared(84129138);
 END IF; RETURN NEW; END $$;
 CREATE TRIGGER inventory_test_pause_transfer AFTER UPDATE ON stock_levels FOR EACH ROW EXECUTE FUNCTION inventory_test_pause_transfer()`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `DROP TRIGGER inventory_test_pause_transfer ON stock_levels; DROP FUNCTION inventory_test_pause_transfer()`); err != nil {
			t.Error(err)
		}
	}()
	one := input
	one.LocationID = &id
	one.IdempotencyKey = "transfer:" + uuid.NewString()
	old := first.Movement.LocationID.String()
	two := secondInput
	two.LocationID = &old
	two.IdempotencyKey = "transfer:" + uuid.NewString()
	results := concurrentChanges(t, pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(84129138)")
		return err
	}, editOperation(one, first.Movement.ID), editOperation(two, second.Movement.ID))
	for _, r := range results {
		if r.err != nil {
			t.Errorf("opposite transfer must serialize without deadlock: %v", r.err)
		}
	}
	if t.Failed() {
		return
	}
	for _, r := range results {
		level, err := q.GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: r.result.Movement.VariantID, LocationID: r.result.Movement.LocationID})
		if err != nil || !level.QuantityOnHand.Equal(decimal.NewFromInt(5)) {
			t.Fatalf("transfer stock=%+v err=%v", level, err)
		}
	}
	storedFirst, err := q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{BusinessID: input.BusinessID, ID: first.Movement.ID})
	if err != nil || storedFirst.LocationID != location.ID {
		t.Fatalf("first association: %+v %v", storedFirst, err)
	}
	storedSecond, err := q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{BusinessID: input.BusinessID, ID: second.Movement.ID})
	if err != nil || storedSecond.LocationID != first.Movement.LocationID {
		t.Fatalf("second association: %+v %v", storedSecond, err)
	}
	assertMovementCounts(t, pool, input.BusinessID, 2, 2, 2, 4)
}
