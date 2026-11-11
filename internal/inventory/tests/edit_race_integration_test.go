package inventory_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

// Observe database waits before releasing the barrier. A start channel alone
// cannot prove the operations overlapped at the contested persistence boundary.
func waitInventoryLocks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, count int) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname=current_database() AND application_name='openlocal_movement_test'
AND wait_event_type='Lock' AND query LIKE $1`, "%"+query+"%").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= count {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("operations did not reach the database barrier")
		case <-ticker.C:
		}
	}
}

func TestEditTransferRejectsConcurrentNegativeDestination(t *testing.T) {
	pool := createMovementPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := createMovementInput(t, pool)
	q := db.New(pool)
	service := movement.New(movementpostgres.New(pool, q))
	created, err := service.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: input.BusinessID, Name: "Concurrent destination"})
	if err != nil {
		t.Fatal(err)
	}
	// This trigger pauses only this fixture's positive destination INSERT after
	// the legacy fallback has already observed the destination row as absent.
	// Concurrent creation still permits negative stock, as its contract requires.
	_, err = pool.Exec(ctx, `CREATE FUNCTION inventory_test_pause_destination() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.location_id = '`+destination.ID.String()+`'::uuid AND NEW.quantity_on_hand = 5 THEN
  PERFORM pg_advisory_xact_lock(84129137);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER inventory_test_pause_destination BEFORE INSERT ON stock_levels
FOR EACH ROW EXECUTE FUNCTION inventory_test_pause_destination()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DROP TRIGGER inventory_test_pause_destination ON stock_levels; DROP FUNCTION inventory_test_pause_destination()`); err != nil {
			t.Error(err)
		}
	})
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock(84129137)"); err != nil {
		t.Fatal(err)
	}
	app, _ := movementHTTPApp(t, pool, input)
	request := editRequest(input)
	id := destination.ID.String()
	request.LocationID = &id
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		status int
		body   string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		req := httptest.NewRequest("PATCH", "/inventory/movements/"+created.Movement.ID.String(), strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "race:"+uuid.NewString())
		res, err := app.Test(req, 12000)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		done <- outcome{res.StatusCode, string(body), err}
	}()
	defer func() {
		_ = blocker.Rollback(context.Background())
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("edit did not terminate")
		}
	}()
	waitInventoryLocks(t, ctx, pool, "INSERT INTO stock_levels", 1)
	concurrent := input
	concurrent.LocationID = &id
	concurrent.MovementType = "OUT_SALE"
	concurrent.Quantity = "10"
	concurrent.IdempotencyKey = "negative:" + uuid.NewString()
	if _, err := service.Create(ctx, concurrent); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-done
	// Keep cleanup's receive nonblocking after consuming the result.
	done <- result
	if result.err != nil || result.status != 409 {
		t.Fatalf("edit bypassed nonnegative policy: status=%d body=%s err=%v", result.status, result.body, result.err)
	}
	stored, err := q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{ID: created.Movement.ID, BusinessID: input.BusinessID})
	if err != nil || stored.LocationID != created.Movement.LocationID {
		t.Fatalf("failed edit moved association: %+v %v", stored, err)
	}
	for _, entry := range []struct {
		location uuid.UUID
		quantity int64
	}{{created.Movement.LocationID, 5}, {destination.ID, -10}} {
		level, err := q.GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: created.Movement.VariantID, LocationID: entry.location})
		if err != nil || !level.QuantityOnHand.Equal(decimal.NewFromInt(entry.quantity)) {
			t.Fatalf("partial write: %+v %v", level, err)
		}
	}
	assertMovementCounts(t, pool, input.BusinessID, 2, 2, 2, 2)
}
