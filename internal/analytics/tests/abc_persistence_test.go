package analytics_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/analytics"
	"github.com/palmieridev/openlocal-api/internal/analytics/analyticspostgres"
	"github.com/palmieridev/openlocal-api/internal/analytics/reporting"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

// Summary refresh and migration DDL take an exclusive view lock. Keep both the
// view and all historical fixtures in a rollback transaction, bounded by server
// timeouts, so other packages using the catalog database retain their snapshot.
func abcSnapshot(t *testing.T) (*fixture, pgx.Tx, context.Context) {
	t.Helper()
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := tx.Rollback(cleanup); err != nil && err != pgx.ErrTxClosed {
			t.Error(err)
		}
	})
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'"); err != nil {
		t.Fatal(err)
	}
	f.q = db.New(tx)
	f.rt.Q = f.q
	f.app = fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	analytics.NewHandler(f.rt).RegisterPrivateRoutes(f.app.Group("", func(c *fiber.Ctx) error {
		auth.SetFiber(c, f.identity)
		return c.Next()
	}))
	return f, tx, ctx
}

func applyAnalyticsMigration(t *testing.T, ctx context.Context, tx pgx.Tx, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(data)); err != nil {
		t.Fatal(err)
	}
}

func legacyAnalyticsSummary(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	applyAnalyticsMigration(t, ctx, tx, "000008_analytics_views.down.sql")
	applyAnalyticsMigration(t, ctx, tx, "000008_analytics_views.up.sql")
}

type abcData struct {
	business         db.Business
	variants         []db.ProductVariant
	foreign          db.ProductVariant
	invalidMovements []uuid.UUID
}

func abcHistory(t *testing.T, ctx context.Context, f *fixture, tx pgx.Tx) abcData {
	t.Helper()
	business := func() db.Business {
		b, err := f.q.CreateBusiness(ctx, db.CreateBusinessParams{
			Name: "ABC fixture", Slug: "abc-" + uuid.NewString(), BusinessType: "retail",
			Status: "draft", Timezone: "America/Mexico_City", LocationMode: "fixed",
		})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	own, other := business(), business()
	f.member(t, own, "owner")
	location := func(b db.Business) uuid.UUID {
		loc, err := f.q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: b.ID, Name: "Default", IsDefault: true})
		if err != nil {
			t.Fatal(err)
		}
		return loc.ID
	}
	ownLocation, otherLocation := location(own), location(other)
	out := abcData{business: own}
	for _, item := range []struct{ name, price string }{{"Alpha", "10"}, {"Beta", "5"}, {"Gamma", "5"}, {"Unsold", "10"}} {
		product, err := f.q.CreateProduct(ctx, db.CreateProductParams{
			BusinessID: own.ID, Name: item.name, Slug: "abc-" + uuid.NewString(),
			Unit: "piece", ProductType: "stocked_product", Status: "active",
		})
		if err != nil {
			t.Fatal(err)
		}
		variant, err := f.q.CreateVariant(ctx, db.CreateVariantParams{
			BusinessID: own.ID, ProductID: product.ID, Sku: uuid.NewString(), InternalCode: uuid.NewString(),
			Name: item.name, Price: decimal.RequireFromString(item.price), Currency: "MXN",
			Attributes: []byte(`{}`), PublicStockStatus: "available", Status: "active",
		})
		if err != nil {
			t.Fatal(err)
		}
		out.variants = append(out.variants, variant)
	}
	out.foreign = f.variant(t, other)
	// Valid write boundaries reject this relation. Model pre-existing malformed
	// data directly, without changing the production ownership guards.
	if _, err := tx.Exec(ctx, "UPDATE product_variants SET product_id=$2 WHERE id=$1", out.foreign.ID, out.variants[0].ProductID); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		business, variant, location uuid.UUID
		kind, quantity              string
	}{
		{own.ID, out.variants[0].ID, ownLocation, "OUT_SALE", "8"},
		{own.ID, out.variants[1].ID, ownLocation, "OUT_SALE", "3"},
		{own.ID, out.variants[2].ID, ownLocation, "OUT_SALE", "1"},
		{own.ID, out.variants[0].ID, ownLocation, "IN_PURCHASE", "99"},
		{other.ID, out.variants[0].ID, otherLocation, "OUT_SALE", "7"},
		{other.ID, out.variants[0].ID, ownLocation, "OUT_SALE", "17"},
		{own.ID, out.variants[0].ID, otherLocation, "OUT_SALE", "11"},
		{other.ID, out.foreign.ID, otherLocation, "OUT_SALE", "13"},
	} {
		item, err := f.q.CreateStockMovement(ctx, db.CreateStockMovementParams{
			BusinessID: row.business, VariantID: row.variant, LocationID: row.location,
			MovementType: row.kind, Quantity: decimal.RequireFromString(row.quantity),
			CreatedBy: uuid.NullUUID{UUID: f.identity.UserID, Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		if row.variant == out.variants[0].ID && (row.business != own.ID || row.location != ownLocation) {
			out.invalidMovements = append(out.invalidMovements, item.ID)
		}
	}
	return out
}

func assertABCReport(t *testing.T, f *fixture, data abcData, values, classes []string) {
	t.Helper()
	for _, path := range []string{"/analytics/abc", "/analytics/pareto"} {
		rows := decode[[]analytics.ABCResponse](t, f.request(t, "GET", path+"?business_id="+data.business.ID.String(), "", 200))
		if len(rows) != len(values) {
			t.Fatalf("%s returned %d products, want %d", path, len(rows), len(values))
		}
		total, cumulative := decimal.Zero, decimal.Zero
		for _, value := range values {
			total = total.Add(decimal.RequireFromString(value))
		}
		for i, row := range rows {
			cumulative = cumulative.Add(decimal.RequireFromString(values[i]))
			if row.ProductID != data.variants[i].ProductID || row.Value != values[i] || row.TotalValue != total.String() || row.CumulativeValue != cumulative.String() || row.Class != classes[i] {
				t.Errorf("%s product %d: %+v; want value=%s total=%s cumulative=%s class=%s", path, i, row, values[i], total, cumulative, classes[i])
			}
		}
	}
}

func TestABCSummaryExcludesForeignTenantsAndLocations(t *testing.T) {
	f, tx, ctx := abcSnapshot(t)
	data := abcHistory(t, ctx, f, tx)
	if _, err := tx.Exec(ctx, "REFRESH MATERIALIZED VIEW product_sales_summary"); err != nil {
		t.Fatal(err)
	}
	assertABCReport(t, f, data, []string{"80", "15", "5", "0"}, []string{"A", "B", "C", "C"})
	assertABCSummary(t, ctx, tx, data)
}

func assertABCSummary(t *testing.T, ctx context.Context, tx pgx.Tx, data abcData) {
	t.Helper()
	var units, revenue decimal.Decimal
	if err := tx.QueryRow(ctx, "SELECT units_sold,sales_revenue FROM product_sales_summary WHERE variant_id=$1", data.variants[0].ID).Scan(&units, &revenue); err != nil {
		t.Fatal(err)
	}
	if !units.Equal(decimal.NewFromInt(8)) || !revenue.Equal(decimal.NewFromInt(80)) {
		t.Errorf("summary contains foreign movements: units=%s revenue=%s", units, revenue)
	}
	var foreignRows int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM product_sales_summary WHERE variant_id=$1", data.foreign.ID).Scan(&foreignRows); err != nil {
		t.Fatal(err)
	}
	if foreignRows != 0 {
		t.Error("summary retains a variant assigned to another business's product")
	}
}

func TestABCQueryRejectsForeignLegacySummaryRows(t *testing.T) {
	f, tx, ctx := abcSnapshot(t)
	// Exercise the reporting predicate independently of the hardened view.
	legacyAnalyticsSummary(t, ctx, tx)
	data := abcHistory(t, ctx, f, tx)
	// Isolate the read-query guard from the source movement guards.
	if _, err := tx.Exec(ctx, "DELETE FROM stock_movements WHERE id=ANY($1::uuid[])", data.invalidMovements); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "REFRESH MATERIALIZED VIEW product_sales_summary"); err != nil {
		t.Fatal(err)
	}
	var foreignRows int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM product_sales_summary WHERE variant_id=$1 AND business_id<>$2", data.foreign.ID, data.business.ID).Scan(&foreignRows); err != nil {
		t.Fatal(err)
	}
	if foreignRows != 1 {
		t.Fatal("legacy fixture did not produce a foreign-business summary row")
	}
	// The legacy view still includes the foreign variant/product relation.
	// Only the read-query tenant predicate can reject that summary row here.
	store := analyticspostgres.New(f.q)
	rows, err := store.ABC(ctx, data.business.ID, reporting.Page{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || !rows[0].Value.Equal(decimal.NewFromInt(80)) || !rows[0].TotalValue.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("foreign-business summary influenced the report: %+v", rows)
	}
}

func TestABCMigrationRepairsCachedSummaryAndPreservesSecureRollback(t *testing.T) {
	f, tx, ctx := abcSnapshot(t)
	legacyAnalyticsSummary(t, ctx, tx)
	data := abcHistory(t, ctx, f, tx)
	if _, err := tx.Exec(ctx, "REFRESH MATERIALIZED VIEW product_sales_summary"); err != nil {
		t.Fatal(err)
	}
	var cached decimal.Decimal
	if err := tx.QueryRow(ctx, "SELECT sales_revenue FROM product_sales_summary WHERE variant_id=$1", data.variants[0].ID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if !cached.Equal(decimal.NewFromInt(430)) {
		t.Fatalf("upgrade fixture lacks the expected contaminated cache: %s", cached)
	}

	applyAnalyticsMigration(t, ctx, tx, "000024_analytics_tenant_consistency.up.sql")
	assertABCSummary(t, ctx, tx, data)
	// The upgrade itself must repair the populated snapshot, before any refresh.
	assertABCReport(t, f, data, []string{"80", "15", "5", "0"}, []string{"A", "B", "C", "C"})
	// Concurrent refresh requires the retained unique index and populated view.
	if _, err := tx.Exec(ctx, "REFRESH MATERIALIZED VIEW CONCURRENTLY product_sales_summary"); err != nil {
		t.Fatal(err)
	}
	assertABCReport(t, f, data, []string{"80", "15", "5", "0"}, []string{"A", "B", "C", "C"})
	var malformedMovements int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE id=ANY($1::uuid[])", data.invalidMovements).Scan(&malformedMovements); err != nil {
		t.Fatal(err)
	}
	if malformedMovements != len(data.invalidMovements) {
		t.Fatal("migration changed source movements instead of excluding their contributions")
	}

	applyAnalyticsMigration(t, ctx, tx, "000024_analytics_tenant_consistency.down.sql")
	assertABCReport(t, f, data, []string{"80", "15", "5", "0"}, []string{"A", "B", "C", "C"})
	applyAnalyticsMigration(t, ctx, tx, "000024_analytics_tenant_consistency.up.sql")
	assertABCReport(t, f, data, []string{"80", "15", "5", "0"}, []string{"A", "B", "C", "C"})
}
