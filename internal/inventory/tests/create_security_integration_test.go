package inventory_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/inventory"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func TestCreateMovementLocationIsolation(t *testing.T) {
	pool := createMovementPool(t)
	service := movement.New(movementpostgres.New(pool, db.New(pool)))
	ctx := context.Background()
	for _, name := range []string{"foreign location", "missing location"} {
		t.Run(name, func(t *testing.T) {
			// Register the foreign fixture first so even a failing implementation
			// cleans up the attempted movement before deleting its location.
			other := createMovementInput(t, pool)
			input := createMovementInput(t, pool)
			foreign, err := db.New(pool).CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: other.BusinessID, Name: "Private warehouse"})
			if err != nil {
				t.Fatal(err)
			}
			locationID := foreign.ID.String()
			if name == "missing location" {
				locationID = uuid.NewString()
			}
			input.LocationID = &locationID
			if _, err := service.Create(ctx, input); !errors.Is(err, movement.ErrLocationNotFound) {
				t.Errorf("location outside the authorized business: error=%v, want location not found", err)
			}
			assertMovementCounts(t, pool, input.BusinessID, 0, 0, 0, 0)
			assertMovementCounts(t, pool, other.BusinessID, 0, 0, 1, 0)

			// A rejected attempt must not consume the idempotency key.
			input.LocationID = nil
			if _, err := service.Create(ctx, input); err != nil {
				t.Fatalf("key unusable after rejection: %v", err)
			}
			assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 1)
		})
	}
}

func TestCreateMovementLocationIsolationHTTP(t *testing.T) {
	pool := createMovementPool(t)
	other := createMovementInput(t, pool)
	input := createMovementInput(t, pool)
	foreign, err := db.New(pool).CreateInventoryLocation(context.Background(), db.CreateInventoryLocationParams{BusinessID: other.BusinessID, Name: "Private warehouse"})
	if err != nil {
		t.Fatal(err)
	}
	app, _ := movementHTTPApp(t, pool, input)
	var previousBody string
	for _, locationID := range []string{foreign.ID.String(), uuid.NewString()} {
		body, err := json.Marshal(inventory.StockMovementRequest{
			BusinessID: input.BusinessID.String(), VariantID: input.VariantID, LocationID: &locationID,
			MovementType: input.MovementType, Quantity: input.Quantity,
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/inventory/movements", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", input.IdempotencyKey)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 404 {
			t.Errorf("location outside the business: status=%d, want 404; body=%s", res.StatusCode, data)
		}
		var failure struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.Unmarshal(data, &failure); err != nil || failure.Error != "inventory location not found" || failure.Code != "inventory_location_not_found" {
			t.Errorf("unexpected location rejection: %s (decode error=%v)", data, err)
		}
		if previousBody != "" && string(data) != previousBody {
			t.Errorf("response distinguishes foreign and nonexistent locations: %s vs %s", previousBody, data)
		}
		previousBody = string(data)
		assertMovementCounts(t, pool, input.BusinessID, 0, 0, 0, 0)
		assertMovementCounts(t, pool, other.BusinessID, 0, 0, 1, 0)
	}
}

func TestCreateMovementConcurrentDefaultLocation(t *testing.T) {
	pool := createMovementPool(t)
	for _, sameKey := range []bool{false, true} {
		name := "distinct movements"
		if sameKey {
			name = "idempotent retries"
		}
		t.Run(name, func(t *testing.T) {
			input := createMovementInput(t, pool)
			second := input
			if !sameKey {
				second.IdempotencyKey = "second:" + uuid.NewString()
			}
			results := concurrentFirstMovements(t, pool, input, second)
			if results[0].Movement.LocationID != results[1].Movement.LocationID {
				t.Fatal("first movements did not resolve the same default location")
			}
			movements, replays := 2, 0
			for _, result := range results {
				if result.Replayed {
					replays++
				}
			}
			if sameKey {
				movements = 1
				if results[0].Movement.ID != results[1].Movement.ID || replays != 1 {
					t.Fatal("concurrent retries must return the same movement with one replay")
				}
			} else if results[0].Movement.ID == results[1].Movement.ID || replays != 0 {
				t.Fatal("distinct keys must each create a movement")
			}
			assertMovementCounts(t, pool, input.BusinessID, movements, 1, 1, movements)
			level, err := db.New(pool).GetStockLevel(context.Background(), db.GetStockLevelParams{
				BusinessID: input.BusinessID, VariantID: uuid.MustParse(input.VariantID), LocationID: results[0].Movement.LocationID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !level.QuantityOnHand.Equal(decimal.NewFromInt(int64(5 * movements))) {
				t.Fatalf("stock=%s, want %d", level.QuantityOnHand, 5*movements)
			}
		})
	}
}

func concurrentFirstMovements(t *testing.T, pool *pgxpool.Pool, inputs ...movement.Input) []movement.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	defer func() {
		_ = blocker.Rollback(context.Background())
		cancel()
		workers.Wait()
	}()
	// SHARE permits the old default-location SELECT but blocks INSERT. Wait for
	// both attempts to reach the database write before releasing the lock, so
	// the race is exercised even on a single CPU without scheduling sleeps.
	if _, err := blocker.Exec(ctx, "LOCK TABLE inventory_locations IN SHARE MODE"); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result movement.Result
		err    error
	}
	completed := make(chan outcome, len(inputs))
	service := movement.New(movementpostgres.New(pool, db.New(pool)))
	for _, input := range inputs {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := service.Create(ctx, input)
			completed <- outcome{result, err}
		}()
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND application_name = 'openlocal_movement_test'
  AND wait_event_type = 'Lock' AND query LIKE '%INSERT INTO inventory_locations%'`).Scan(&blocked)
		if err != nil {
			t.Fatalf("observe concurrent location writes: %v", err)
		}
		if blocked == len(inputs) {
			break
		}
		select {
		case failure := <-completed:
			t.Fatalf("request returned before the concurrency barrier: %v", failure.err)
		case <-ctx.Done():
			t.Fatal("requests did not reach concurrent location writes")
		case <-ticker.C:
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var results []movement.Result
	for range inputs {
		outcome := <-completed
		if outcome.err != nil {
			t.Errorf("concurrent first movement: %v", outcome.err)
		}
		results = append(results, outcome.result)
	}
	if t.Failed() {
		t.FailNow()
	}
	return results
}
