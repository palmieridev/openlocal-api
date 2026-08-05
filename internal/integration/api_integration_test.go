package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

func TestBusinessHoursCanBeReplaced(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)
	business := createBusiness(t, ctx, q)

	_, err = q.UpsertBusinessHour(ctx, db.UpsertBusinessHourParams{
		BusinessID: business.ID,
		DayOfWeek:  1,
		OpensAt:    pgtype.Time{Microseconds: 9 * 60 * 60 * 1_000_000, Valid: true},
		ClosesAt:   pgtype.Time{Microseconds: 18 * 60 * 60 * 1_000_000, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.UpsertBusinessHour(ctx, db.UpsertBusinessHourParams{
		BusinessID: business.ID,
		DayOfWeek:  2,
		IsClosed:   true,
	}); err != nil {
		t.Fatal(err)
	}
	hours, err := q.ListBusinessHours(ctx, business.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 2 || hours[0].DayOfWeek != 1 || !hours[1].IsClosed {
		t.Fatalf("unexpected business hours: %#v", hours)
	}
	if err := q.DeleteBusinessHours(ctx, business.ID); err != nil {
		t.Fatal(err)
	}
	hours, err = q.ListBusinessHours(ctx, business.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 0 {
		t.Fatalf("expected no business hours, got %d", len(hours))
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
		City:              sql.NullString{String: "CDMX", Valid: true},
		State:             sql.NullString{String: "CDMX", Valid: true},
		Country:           sql.NullString{String: "MX", Valid: true},
		PickupAvailable:   true,
		DeliveryAvailable: false,
		Timezone:          "America/Mexico_City",
		LocationMode:      "fixed",
	})
	if err != nil {
		t.Fatal(err)
	}
	return business
}

func TestMobileBusinessReachFiltersBusinessesAndProducts(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)

	suffix := uuid.NewString()[:8]
	mobile, err := q.CreateBusiness(ctx, db.CreateBusinessParams{
		Name: "Mobile Integration", Slug: "mobile-integration-" + suffix,
		Description: "", BusinessType: "servicios", Status: "active",
		Timezone: "America/Mexico_City", LocationMode: "mobile",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = q.CreateBusinessServiceArea(ctx, db.CreateBusinessServiceAreaParams{
		BusinessID: mobile.ID, Name: "Coyoacán", Country: "MX", State: "Ciudad de México",
		Municipality: sql.NullString{String: "Coyoacán", Valid: true},
		CountryKey:   "mx", StateKey: "ciudad-de-mexico",
		MunicipalityKey: sql.NullString{String: "coyoacan", Valid: true},
		NormalizedKey:   "mx|ciudad-de-mexico|coyoacan|||",
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := q.CreateProduct(ctx, db.CreateProductParams{
		BusinessID: mobile.ID, Name: "Mueble móvil", Slug: "mobile-furniture-" + suffix,
		Description: "", Unit: "proyecto", ProductType: "made_to_order_product",
		IsPublic: true, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	variant, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ProductID: product.ID, BusinessID: mobile.ID, Sku: "MOBILE-" + suffix,
		InternalCode: "MOBILE-" + suffix, Name: "Cotización", Attributes: []byte(`{}`),
		Price: decimal.NewFromInt(1000), Currency: "MXN", PublicStockStatus: "made_to_order",
		Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	matching := db.ListPublicBusinessesParams{
		HasAreaFilter:   true,
		State:           sql.NullString{String: "Ciudad de México", Valid: true},
		Municipality:    sql.NullString{String: "Coyoacán", Valid: true},
		StateKey:        sql.NullString{String: "ciudad-de-mexico", Valid: true},
		MunicipalityKey: sql.NullString{String: "coyoacan", Valid: true},
		LimitCount:      100,
	}
	businessRows, err := q.ListPublicBusinesses(ctx, matching)
	if err != nil {
		t.Fatal(err)
	}
	foundBusiness := false
	for _, row := range businessRows {
		foundBusiness = foundBusiness || row.ID == mobile.ID
	}
	if !foundBusiness {
		t.Fatal("mobile business did not match its service area")
	}

	productRows, err := q.SearchMarketplaceProducts(ctx, db.SearchMarketplaceProductsParams{
		SearchQuery: "", HasAreaFilter: true,
		State: matching.State, Municipality: matching.Municipality,
		StateKey: matching.StateKey, MunicipalityKey: matching.MunicipalityKey,
		LimitCount: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundVariant := false
	for _, row := range productRows {
		foundVariant = foundVariant || row.VariantID == variant.ID
	}
	if !foundVariant {
		t.Fatal("mobile product did not match its service area")
	}

	matching.Municipality = sql.NullString{String: "Tlalpan", Valid: true}
	matching.MunicipalityKey = sql.NullString{String: "tlalpan", Valid: true}
	businessRows, err = q.ListPublicBusinesses(ctx, matching)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range businessRows {
		if row.ID == mobile.ID {
			t.Fatal("mobile business matched an unrelated service area")
		}
	}
}

func TestVariantImageCanBeSetReplacedAndCleared(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)

	business := createBusiness(t, ctx, q)
	product, err := q.CreateProduct(ctx, db.CreateProductParams{
		BusinessID:  business.ID,
		Name:        "Image Product",
		Slug:        "image-product-" + uuid.NewString()[:8],
		Description: "",
		Unit:        "piece",
		ProductType: "stocked_product",
		IsPublic:    true,
		Status:      "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	barcode := uuid.NewString()
	variant, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ProductID:         product.ID,
		BusinessID:        business.ID,
		Sku:               "IMG-" + uuid.NewString()[:8],
		Barcode:           sql.NullString{String: barcode, Valid: true},
		InternalCode:      "IMG-" + uuid.NewString()[:8],
		Name:              "Image Variant",
		Attributes:        []byte(`{}`),
		Price:             decimal.NewFromInt(100),
		Currency:          "MXN",
		TrackInventory:    true,
		PublicStockStatus: "available",
		ReorderPoint:      decimal.Zero,
		Status:            "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	// No image yet: the read must yield "" rather than ErrNoRows.
	current, err := q.GetVariantImage(ctx, variant.ID)
	if err != nil {
		t.Fatalf("GetVariantImage on an imageless variant: %v", err)
	}
	if current != "" {
		t.Fatalf("got %q, want empty string", current)
	}

	first := "https://example.com/first.jpg"
	if err := q.CreateVariantImage(ctx, db.CreateVariantImageParams{VariantID: variant.ID, Url: first}); err != nil {
		t.Fatal(err)
	}
	if current, err = q.GetVariantImage(ctx, variant.ID); err != nil || current != first {
		t.Fatalf("got (%q, %v), want %q", current, err, first)
	}

	// Replace: delete-then-insert must leave exactly one image.
	second := "https://example.com/second.jpg"
	if err := q.DeleteVariantImages(ctx, variant.ID); err != nil {
		t.Fatal(err)
	}
	if err := q.CreateVariantImage(ctx, db.CreateVariantImageParams{VariantID: variant.ID, Url: second}); err != nil {
		t.Fatal(err)
	}
	if current, err = q.GetVariantImage(ctx, variant.ID); err != nil || current != second {
		t.Fatalf("got (%q, %v), want %q", current, err, second)
	}

	byID, err := q.GetVariantForBusiness(ctx, db.GetVariantForBusinessParams{ID: variant.ID, BusinessID: business.ID})
	if err != nil {
		t.Fatal(err)
	}
	if byID.ImageUrl != second {
		t.Fatalf("GetVariantForBusiness image_url = %q, want %q", byID.ImageUrl, second)
	}

	bySKU, err := q.GetVariantBySKU(ctx, db.GetVariantBySKUParams{BusinessID: business.ID, Sku: variant.Sku})
	if err != nil || bySKU.ImageUrl != second {
		t.Fatalf("GetVariantBySKU image_url = %q, err = %v", bySKU.ImageUrl, err)
	}
	byBarcode, err := q.GetVariantByBarcode(ctx, db.GetVariantByBarcodeParams{BusinessID: business.ID, Barcode: variant.Barcode})
	if err != nil || byBarcode.ImageUrl != second {
		t.Fatalf("GetVariantByBarcode image_url = %q, err = %v", byBarcode.ImageUrl, err)
	}

	rows, err := q.ListVariantsByProduct(ctx, db.ListVariantsByProductParams{ProductID: product.ID, BusinessID: business.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ImageUrl != second {
		t.Fatalf("ListVariantsByProduct rows = %#v, want image_url %q", rows, second)
	}

	publicRows, err := q.ListPublicProductsByBusinessSlug(ctx, db.ListPublicProductsByBusinessSlugParams{Slug: business.Slug, Limit: 10, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(publicRows) != 1 || publicRows[0].ImageUrl != second {
		t.Fatalf("ListPublicProductsByBusinessSlug rows = %#v, want image_url %q", publicRows, second)
	}

	marketplaceRows, err := q.SearchMarketplaceProducts(ctx, db.SearchMarketplaceProductsParams{SearchQuery: "", LimitCount: 10, OffsetCount: 0})
	if err != nil {
		t.Fatal(err)
	}
	var marketplaceImage string
	for _, row := range marketplaceRows {
		if row.VariantID == variant.ID {
			marketplaceImage = row.ImageUrl
		}
	}
	if marketplaceImage != second {
		t.Fatalf("SearchMarketplaceProducts image_url = %q, want %q", marketplaceImage, second)
	}

	// Clear.
	if err := q.DeleteVariantImages(ctx, variant.ID); err != nil {
		t.Fatal(err)
	}
	if current, err = q.GetVariantImage(ctx, variant.ID); err != nil || current != "" {
		t.Fatalf("got (%q, %v), want empty string", current, err)
	}
}
