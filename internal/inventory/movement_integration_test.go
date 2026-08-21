package inventory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

type movementFixture struct {
	q        *db.Queries
	business db.Business
	variant  db.ProductVariant
	location db.InventoryLocation
	movement db.StockMovement
}

func TestMovementEditsAndDeletesUpdateStockTransactionally(t *testing.T) {
	pool := movementIntegrationPool(t)

	t.Run("edit up", func(t *testing.T) {
		fixture := newMovementFixture(t, pool, decimal.NewFromInt(5))
		params := movementUpdateParams(fixture.movement, decimal.NewFromInt(8), "edit-up:"+uuid.NewString())

		level, err := applyMovementEditDelta(context.Background(), fixture.q, fixture.movement, params)
		if err != nil {
			t.Fatal(err)
		}
		movement, err := fixture.q.UpdateStockMovement(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		if !level.QuantityOnHand.Equal(decimal.NewFromInt(8)) || !movement.Quantity.Equal(decimal.NewFromInt(8)) {
			t.Fatalf("edit up was not applied atomically: level=%s movement=%s", level.QuantityOnHand, movement.Quantity)
		}
	})

	t.Run("edit down", func(t *testing.T) {
		fixture := newMovementFixture(t, pool, decimal.NewFromInt(5))
		params := movementUpdateParams(fixture.movement, decimal.NewFromInt(2), "edit-down:"+uuid.NewString())

		level, err := applyMovementEditDelta(context.Background(), fixture.q, fixture.movement, params)
		if err != nil {
			t.Fatal(err)
		}
		movement, err := fixture.q.UpdateStockMovement(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		if !level.QuantityOnHand.Equal(decimal.NewFromInt(2)) || !movement.Quantity.Equal(decimal.NewFromInt(2)) {
			t.Fatalf("edit down was not applied atomically: level=%s movement=%s", level.QuantityOnHand, movement.Quantity)
		}
	})

	t.Run("delete", func(t *testing.T) {
		fixture := newMovementFixture(t, pool, decimal.NewFromInt(5))

		level, err := applyNonnegativeStockDelta(context.Background(), fixture.q,
			fixture.business.ID, fixture.variant.ID, fixture.location.ID,
			SignedQuantity(fixture.movement.MovementType, fixture.movement.Quantity).Neg())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.q.DeleteStockMovement(context.Background(), db.DeleteStockMovementParams{
			ID: fixture.movement.ID, BusinessID: fixture.business.ID,
		}); err != nil {
			t.Fatal(err)
		}
		if !level.QuantityOnHand.IsZero() {
			t.Fatalf("delete did not reverse stock: got %s", level.QuantityOnHand)
		}
		if _, err := fixture.q.GetStockMovementForUpdate(context.Background(), db.GetStockMovementForUpdateParams{
			ID: fixture.movement.ID, BusinessID: fixture.business.ID,
		}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("deleted movement is still present: %v", err)
		}
	})

	t.Run("negative stock boundary", func(t *testing.T) {
		fixture := newMovementFixture(t, pool, decimal.NewFromInt(5))
		outgoing, err := fixture.q.CreateStockMovement(context.Background(), db.CreateStockMovementParams{
			BusinessID: fixture.business.ID, VariantID: fixture.variant.ID, LocationID: fixture.location.ID,
			MovementType: "OUT_SALE", Quantity: decimal.NewFromInt(4),
			CreatedBy:      fixture.movement.CreatedBy,
			IdempotencyKey: sql.NullString{String: "consume:" + uuid.NewString(), Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		level, err := fixture.q.ApplyStockDelta(context.Background(), db.ApplyStockDeltaParams{
			BusinessID: fixture.business.ID, VariantID: fixture.variant.ID, LocationID: fixture.location.ID,
			QuantityOnHand: SignedQuantity(outgoing.MovementType, outgoing.Quantity),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !level.QuantityOnHand.Equal(decimal.NewFromInt(1)) {
			t.Fatalf("test setup stock = %s, want 1", level.QuantityOnHand)
		}

		params := movementUpdateParams(fixture.movement, decimal.RequireFromString("0.5"), "negative:"+uuid.NewString())
		_, err = applyMovementEditDelta(context.Background(), fixture.q, fixture.movement, params)
		var fiberErr *fiber.Error
		if !errors.As(err, &fiberErr) || fiberErr.Code != fiber.StatusConflict {
			t.Fatalf("negative edit error = %v, want 409 conflict", err)
		}
		storedLevel, err := fixture.q.GetStockLevel(context.Background(), db.GetStockLevelParams{
			BusinessID: fixture.business.ID, VariantID: fixture.variant.ID, LocationID: fixture.location.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		storedMovement, err := fixture.q.GetStockMovementForUpdate(context.Background(), db.GetStockMovementForUpdateParams{
			ID: fixture.movement.ID, BusinessID: fixture.business.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !storedLevel.QuantityOnHand.Equal(decimal.NewFromInt(1)) || !storedMovement.Quantity.Equal(decimal.NewFromInt(5)) {
			t.Fatalf("rejected edit changed state: level=%s movement=%s", storedLevel.QuantityOnHand, storedMovement.Quantity)
		}
	})
}

func movementIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("OPENLOCAL_INTEGRATION_TESTS") != "1" {
		t.Skip("set OPENLOCAL_INTEGRATION_TESTS=1 to run database integration tests")
	}
	_ = godotenv.Load("../../.env.local")
	_ = godotenv.Load("../../.env")
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://openlocal:openlocal@localhost:5432/openlocal?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newMovementFixture(t *testing.T, pool *pgxpool.Pool, quantity decimal.Decimal) movementFixture {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)
	user, err := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{
		ClerkUserID: "user_" + uuid.NewString(),
		Email:       sql.NullString{String: uuid.NewString() + "@example.com", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	business, err := q.CreateBusiness(ctx, db.CreateBusinessParams{
		Name: "Movement Test", Slug: "movement-test-" + uuid.NewString()[:8], Description: "",
		BusinessType: "retail", Status: "active", Country: sql.NullString{String: "MX", Valid: true},
		Timezone: "America/Mexico_City", LocationMode: "fixed",
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := q.CreateProduct(ctx, db.CreateProductParams{
		BusinessID: business.ID, Name: "Movement Product", Slug: "movement-product-" + uuid.NewString()[:8],
		Description: "", Unit: "piece", ProductType: "stocked_product", IsPublic: true, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	variant, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ProductID: product.ID, BusinessID: business.ID, Sku: "SKU-" + uuid.NewString()[:8],
		InternalCode: "INT-" + uuid.NewString()[:8], Name: "Default", Attributes: []byte(`{}`),
		Price: decimal.NewFromInt(100), Currency: "MXN", TrackInventory: true,
		PublicStockStatus: "available", IsPublic: true, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	location, err := q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{
		BusinessID: business.ID, Name: "Default", IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	movement, err := q.CreateStockMovement(ctx, db.CreateStockMovementParams{
		BusinessID: business.ID, VariantID: variant.ID, LocationID: location.ID,
		MovementType: "IN_PURCHASE", Quantity: quantity,
		CreatedBy:      uuid.NullUUID{UUID: user.ID, Valid: true},
		IdempotencyKey: sql.NullString{String: "create:" + uuid.NewString(), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.ApplyStockDelta(ctx, db.ApplyStockDeltaParams{
		BusinessID: business.ID, VariantID: variant.ID, LocationID: location.ID, QuantityOnHand: quantity,
	}); err != nil {
		t.Fatal(err)
	}
	return movementFixture{q: q, business: business, variant: variant, location: location, movement: movement}
}

func movementUpdateParams(movement db.StockMovement, quantity decimal.Decimal, idempotencyKey string) db.UpdateStockMovementParams {
	return db.UpdateStockMovementParams{
		ID: movement.ID, BusinessID: movement.BusinessID, LocationID: movement.LocationID,
		MovementType: movement.MovementType, Quantity: quantity, UnitCost: movement.UnitCost,
		ReferenceType: movement.ReferenceType, ReferenceID: movement.ReferenceID, Notes: movement.Notes,
		IdempotencyKey: sql.NullString{String: idempotencyKey, Valid: true},
	}
}
