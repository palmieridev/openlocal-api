package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
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

func TestBusinessMembershipIsScopedByClerkOrg(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)

	user := createUser(t, ctx, q)
	business := createBusiness(t, ctx, q)
	member, err := q.AddBusinessMember(ctx, db.AddBusinessMemberParams{
		BusinessID: business.ID,
		UserID:     user.ID,
		ClerkOrgID: "org_allowed_" + uuid.NewString(),
		Role:       "owner",
	})
	if err != nil {
		t.Fatal(err)
	}

	role, err := q.GetBusinessMemberRole(ctx, db.GetBusinessMemberRoleParams{
		BusinessID: business.ID,
		UserID:     user.ID,
		ClerkOrgID: member.ClerkOrgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if role != "owner" {
		t.Fatalf("expected owner role, got %q", role)
	}

	_, err = q.GetBusinessMemberRole(ctx, db.GetBusinessMemberRoleParams{
		BusinessID: business.ID,
		UserID:     user.ID,
		ClerkOrgID: "org_wrong_" + uuid.NewString(),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected org-scoped lookup to fail with ErrNoRows, got %v", err)
	}
}

func TestStockMovementAndStockLevelCanBeAppliedTransactionally(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)

	user := createUser(t, ctx, q)
	business := createBusiness(t, ctx, q)
	product, err := q.CreateProduct(ctx, db.CreateProductParams{
		BusinessID:  business.ID,
		CategoryID:  uuid.NullUUID{},
		Name:        "Integration Product",
		Slug:        "integration-product-" + uuid.NewString()[:8],
		Description: "",
		Brand:       sql.NullString{},
		Unit:        "piece",
		ProductType: "stocked_product",
		IsHandmade:  false,
		IsPublic:    true,
		Status:      "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	variant, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ProductID:         product.ID,
		BusinessID:        business.ID,
		Sku:               "SKU-" + uuid.NewString()[:8],
		Barcode:           sql.NullString{},
		InternalCode:      "INT-" + uuid.NewString()[:8],
		Name:              "Default",
		Attributes:        []byte(`{}`),
		Price:             decimal.NewFromInt(100),
		Cost:              decimal.NullDecimal{Decimal: decimal.NewFromInt(60), Valid: true},
		Currency:          "MXN",
		TrackInventory:    true,
		PublicStockStatus: "available",
		ReorderPoint:      decimal.NewFromInt(2),
		LeadTimeDays:      3,
		Status:            "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	location, err := q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{
		BusinessID: business.ID,
		Name:       "Integration Default",
		IsDefault:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	idempotencyKey := sql.NullString{String: "movement:" + uuid.NewString(), Valid: true}
	movementParams := db.CreateStockMovementParams{
		BusinessID:     business.ID,
		VariantID:      variant.ID,
		LocationID:     location.ID,
		MovementType:   "IN_PURCHASE",
		Quantity:       decimal.NewFromInt(5),
		UnitCost:       decimal.NullDecimal{Decimal: decimal.NewFromInt(60), Valid: true},
		Notes:          "integration movement",
		CreatedBy:      uuid.NullUUID{UUID: user.ID, Valid: true},
		IdempotencyKey: idempotencyKey,
	}
	movement, err := q.CreateStockMovement(ctx, movementParams)
	if err != nil {
		t.Fatal(err)
	}
	level, err := q.ApplyStockDelta(ctx, db.ApplyStockDeltaParams{
		BusinessID:     business.ID,
		VariantID:      variant.ID,
		LocationID:     location.ID,
		QuantityOnHand: movement.Quantity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !level.QuantityOnHand.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("expected quantity_on_hand=5, got %s", level.QuantityOnHand)
	}
	if _, err := q.CreateStockMovement(ctx, movementParams); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected duplicate idempotency key to return ErrNoRows, got %v", err)
	}
	stored, err := q.GetStockMovementByIdempotencyKey(ctx, db.GetStockMovementByIdempotencyKeyParams{
		BusinessID:     business.ID,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != movement.ID {
		t.Fatalf("idempotency lookup returned movement %s, want %s", stored.ID, movement.ID)
	}
}

func createUser(t *testing.T, ctx context.Context, q *db.Queries) db.User {
	t.Helper()
	user, err := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{
		ClerkUserID: "user_" + uuid.NewString(),
		Email:       sql.NullString{String: uuid.NewString() + "@example.com", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func createBusiness(t *testing.T, ctx context.Context, q *db.Queries) db.Business {
	t.Helper()
	slug := "integration-" + uuid.NewString()[:8]
	business, err := q.CreateBusiness(ctx, db.CreateBusinessParams{
		Name:              "Integration Business",
		Slug:              slug,
		Description:       "",
		BusinessType:      "retail",
		Status:            "active",
		City:              "CDMX",
		State:             "CDMX",
		Country:           "MX",
		PickupAvailable:   true,
		DeliveryAvailable: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return business
}
