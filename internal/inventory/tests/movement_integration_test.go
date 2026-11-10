package inventory_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/inventory"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func TestMovementEditsAndDeletesUpdateStockTransactionally(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	q := db.New(pool)
	service := movement.New(movementpostgres.New(pool, q))
	for _, test := range []struct {
		name, method, quantity string
		consume                bool
		status, movements      int
		stock, storedQuantity  int64
		audits                 int
	}{
		{"edit up", "PATCH", "8", false, 200, 1, 8, 8, 2},
		{"edit down", "PATCH", "2", false, 200, 1, 2, 2, 2},
		{"delete", "DELETE", "", false, 204, 0, 0, 0, 2},
		{"negative edit", "PATCH", "0.5", true, 409, 2, 1, 5, 2},
		{"negative delete", "DELETE", "", true, 409, 2, 1, 5, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := createMovementInput(t, pool)
			created, err := service.Create(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if test.consume {
				outgoing := input
				outgoing.MovementType, outgoing.Quantity = "OUT_SALE", "4"
				outgoing.IdempotencyKey = "consume:" + uuid.NewString()
				if _, err := service.Create(ctx, outgoing); err != nil {
					t.Fatal(err)
				}
			}
			app, _ := movementHTTPApp(t, pool, input)
			body, err := json.Marshal(inventory.StockMovementRequest{
				BusinessID: input.BusinessID.String(), VariantID: input.VariantID,
				MovementType: input.MovementType, Quantity: test.quantity,
			})
			if err != nil {
				t.Fatal(err)
			}
			path := "/inventory/movements/" + created.Movement.ID.String()
			if test.method == "DELETE" {
				path += "?business_id=" + input.BusinessID.String()
			}
			req := httptest.NewRequest(test.method, path, strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "mutate:"+uuid.NewString())
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != test.status {
				t.Fatalf("status=%d, want %d; body=%s", res.StatusCode, test.status, data)
			}
			level, err := q.GetStockLevel(ctx, db.GetStockLevelParams{
				BusinessID: input.BusinessID, VariantID: created.Movement.VariantID, LocationID: created.Movement.LocationID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !level.QuantityOnHand.Equal(decimal.NewFromInt(test.stock)) {
				t.Fatalf("persisted stock=%s, want %d", level.QuantityOnHand, test.stock)
			}
			stored, err := q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{ID: created.Movement.ID, BusinessID: input.BusinessID})
			if test.status == 204 {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("deleted movement still present: %v", err)
				}
			} else if err != nil || !stored.Quantity.Equal(decimal.NewFromInt(test.storedQuantity)) {
				t.Fatalf("persisted movement quantity=%s, want %d; error=%v", stored.Quantity, test.storedQuantity, err)
			}
			assertMovementCounts(t, pool, input.BusinessID, test.movements, 1, 1, test.audits)
		})
	}
}
