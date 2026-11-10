package inventory_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/inventory"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

// This suite commits real transactions and installs a failure trigger. Run only
// against an explicitly provisioned local disposable database, never DATABASE_URL.
func createMovementPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("OPENLOCAL_MOVEMENT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set OPENLOCAL_MOVEMENT_TEST_DATABASE_URL to a migrated local disposable openlocal_movement_test database")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if (config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost") || !strings.HasPrefix(config.ConnConfig.Database, "openlocal_movement_test") {
		t.Fatal("movement tests require a local disposable openlocal_movement_test database")
	}
	config.ConnConfig.RuntimeParams["application_name"] = "openlocal_movement_test"
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return pool
}

func createMovementInput(t *testing.T, pool *pgxpool.Pool) movement.Input {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
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

	t.Cleanup(func() {
		// Delete restricted movements before their business, then remove the actor.
		if _, err := pool.Exec(ctx, "DELETE FROM stock_movements WHERE business_id=$1", business.ID); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM businesses WHERE id=$1", business.ID); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user.ID); err != nil {
			t.Error(err)
		}
	})
	return movement.Input{BusinessID: business.ID, ActorID: user.ID, VariantID: variant.ID.String(), MovementType: "IN_PURCHASE", Quantity: "5", IdempotencyKey: "create:" + uuid.NewString()}
}

func assertMovementCounts(t *testing.T, pool *pgxpool.Pool, businessID uuid.UUID, movements, levels, locations, audits int) {
	t.Helper()
	var m, s, l, a int
	err := pool.QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM stock_movements WHERE business_id=$1),
 (SELECT count(*) FROM stock_levels WHERE business_id=$1),
 (SELECT count(*) FROM inventory_locations WHERE business_id=$1),
 (SELECT count(*) FROM audit_logs WHERE business_id=$1)`, businessID).Scan(&m, &s, &l, &a)
	if err != nil {
		t.Fatal(err)
	}
	if m != movements || s != levels || l != locations || a != audits {
		t.Fatalf("counts movement/stock/location/audit = %d/%d/%d/%d, want %d/%d/%d/%d", m, s, l, a, movements, levels, locations, audits)
	}
}

func TestCreateMovementPostgres(t *testing.T) {
	pool := createMovementPool(t)
	service := movement.New(movementpostgres.New(pool, db.New(pool)))
	ctx := context.Background()
	t.Run("create and replay current stock", func(t *testing.T) {
		input := createMovementInput(t, pool)
		first, err := service.Create(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if first.Replayed || !first.StockLevel.QuantityOnHand.Equal(decimal.NewFromInt(5)) {
			t.Fatalf("unexpected create: %+v", first)
		}
		assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 1)
		another := input
		another.IdempotencyKey = "next:" + uuid.NewString()
		another.MovementType = "OUT_SALE"
		another.Quantity = "7"
		if _, err := service.Create(ctx, another); err != nil {
			t.Fatal(err)
		}
		replay, err := service.Create(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if !replay.Replayed || replay.Movement.ID != first.Movement.ID || !replay.StockLevel.QuantityOnHand.Equal(decimal.NewFromInt(-2)) {
			t.Fatalf("replay must return original movement and current stock: %+v", replay)
		}
		assertMovementCounts(t, pool, input.BusinessID, 2, 1, 1, 2)
		changed := input
		changed.Quantity = "6"
		if _, err := service.Create(ctx, changed); !errors.Is(err, movement.ErrConflict) {
			t.Fatalf("changed payload: %v", err)
		}
		changed = input
		changed.ActorID = uuid.New()
		if _, err := service.Create(ctx, changed); !errors.Is(err, movement.ErrConflict) {
			t.Fatalf("changed actor: %v", err)
		}
		assertMovementCounts(t, pool, input.BusinessID, 2, 1, 1, 2)
	})
	t.Run("business scoped variant", func(t *testing.T) {
		input := createMovementInput(t, pool)
		other := createMovementInput(t, pool)
		input.VariantID = other.VariantID
		if _, err := service.Create(ctx, input); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("foreign variant: %v", err)
		}
		assertMovementCounts(t, pool, input.BusinessID, 0, 0, 0, 0)
	})
	t.Run("explicit location", func(t *testing.T) {
		input := createMovementInput(t, pool)
		location, err := db.New(pool).CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: input.BusinessID, Name: "Warehouse"})
		if err != nil {
			t.Fatal(err)
		}
		id := location.ID.String()
		input.LocationID = &id
		result, err := service.Create(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if result.Movement.LocationID != location.ID {
			t.Fatal("requested location lost")
		}
		assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 1)
	})
	t.Run("stock failure rolls movement back", func(t *testing.T) {
		input := createMovementInput(t, pool)
		input.Quantity = "999999999.999"
		if _, err := service.Create(ctx, input); err != nil {
			t.Fatal(err)
		}
		input.IdempotencyKey = "overflow:" + uuid.NewString()
		input.Quantity = "1"
		if _, err := service.Create(ctx, input); err == nil {
			t.Fatal("expected stock overflow")
		}
		assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 1)
		input.MovementType = "OUT_SALE"
		if _, err := service.Create(ctx, input); err != nil {
			t.Fatalf("rolled-back key should be reusable: %v", err)
		}
	})
	t.Run("audit failure rolls all writes back", func(t *testing.T) {
		input := createMovementInput(t, pool)
		// Scope fault injection to this fixture; other packages may be testing
		// stock movements against the same disposable database concurrently.
		if _, err := pool.Exec(ctx, "UPDATE businesses SET description=$2 WHERE id=$1", input.BusinessID, "movement-test-reject-audit"); err != nil {
			t.Fatal(err)
		}
		_, err := pool.Exec(ctx, `CREATE FUNCTION reject_movement_test_audit() RETURNS trigger LANGUAGE plpgsql AS $$
  BEGIN
    IF NEW.action = 'inventory.movement.create' AND EXISTS (
      SELECT 1 FROM businesses WHERE id = NEW.business_id AND description = 'movement-test-reject-audit'
    ) THEN RAISE EXCEPTION 'test audit failure'; END IF;
    RETURN NEW;
  END $$;
  CREATE TRIGGER reject_movement_test_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_movement_test_audit()`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := pool.Exec(ctx, `DROP TRIGGER reject_movement_test_audit ON audit_logs; DROP FUNCTION reject_movement_test_audit()`); err != nil {
				t.Error(err)
			}
		}()
		if _, err := service.Create(ctx, input); err == nil || !strings.Contains(err.Error(), "test audit failure") {
			t.Fatalf("expected audit failure: %v", err)
		}
		assertMovementCounts(t, pool, input.BusinessID, 0, 0, 0, 0)
	})
}

func TestCreateMovementHTTP(t *testing.T) {
	pool := createMovementPool(t)
	input := createMovementInput(t, pool)
	app, identity := movementHTTPApp(t, pool, input)
	userIdentity := *identity
	body, _ := json.Marshal(inventory.StockMovementRequest{BusinessID: input.BusinessID.String(), VariantID: input.VariantID, MovementType: input.MovementType, Quantity: input.Quantity})
	request := func(payload []byte, key string, status int, replayed bool) {
		t.Helper()
		req := httptest.NewRequest("POST", "/inventory/movements", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status || (res.Header.Get("Idempotent-Replayed") == "true") != replayed {
			t.Fatalf("status/header mismatch: %d %v %s", res.StatusCode, res.Header, data)
		}
		if status == 201 || status == 200 {
			var decoded map[string]map[string]json.RawMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded) != 2 || len(decoded["movement"]) != 7 || len(decoded["stock_level"]) != 5 {
				t.Fatalf("unexpected response projection: %s", data)
			}
			for _, key := range []string{"business_id", "created_by", "idempotency_key"} {
				if _, ok := decoded["movement"][key]; ok {
					t.Fatalf("leaked %s", key)
				}
			}
		}
	}
	identity.ClerkOrgID = "org_wrong"
	request(body, input.IdempotencyKey, 403, false)
	*identity = auth.Context{}
	request(body, input.IdempotencyKey, 401, false)
	*identity = userIdentity
	request(body, "bad", 400, false)
	request([]byte(`{"unknown":true}`), input.IdempotencyKey, 400, false)
	assertMovementCounts(t, pool, input.BusinessID, 0, 0, 0, 0)
	request(body, input.IdempotencyKey, 201, false)
	request(body, input.IdempotencyKey, 200, true)
	request([]byte(strings.Replace(string(body), `"quantity":"5"`, `"quantity":"6"`, 1)), input.IdempotencyKey, 409, false)
	assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 1)
}

func movementHTTPApp(t *testing.T, pool *pgxpool.Pool, input movement.Input) (*fiber.App, *auth.Context) {
	t.Helper()
	q := db.New(pool)
	user, err := q.GetUserByID(context.Background(), input.ActorID)
	if err != nil {
		t.Fatal(err)
	}
	org := "org_" + uuid.NewString()
	if _, err := q.AddBusinessMember(context.Background(), db.AddBusinessMemberParams{BusinessID: input.BusinessID, UserID: input.ActorID, ClerkOrgID: org, Role: "staff"}); err != nil {
		t.Fatal(err)
	}
	identity := auth.Context{ClerkUserID: user.ClerkUserID, ClerkOrgID: org, Role: "staff"}
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	// Substitute only the verified identity boundary; local membership checks run normally.
	app.Use(func(c *fiber.Ctx) error { auth.SetFiber(c, identity); return c.Next() })
	inventory.NewHandler(api.Runtime{Pool: pool, Q: q}).RegisterPrivateRoutes(app)
	return app, &identity
}
