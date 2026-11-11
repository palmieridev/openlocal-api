package analytics_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/analytics"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
	"testing"
)

func (f *fixture) member(t *testing.T, b db.Business, role string) {
	t.Helper()
	_, err := f.q.AddBusinessMember(context.Background(), db.AddBusinessMemberParams{BusinessID: b.ID, UserID: f.identity.UserID, ClerkOrgID: f.identity.ClerkOrgID, Role: role})
	if err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) variant(t *testing.T, b db.Business) db.ProductVariant {
	t.Helper()
	ctx := context.Background()
	p, err := f.q.CreateProduct(ctx, db.CreateProductParams{BusinessID: b.ID, Name: "Analytics fixture", Slug: "analytics-" + uuid.NewString(), Unit: "piece", ProductType: "stocked_product", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.q.CreateVariant(ctx, db.CreateVariantParams{BusinessID: b.ID, ProductID: p.ID, Sku: uuid.NewString(), InternalCode: uuid.NewString(), Name: "Variant", Price: decimal.NewFromInt(10), Currency: "MXN", TrackInventory: true, PublicStockStatus: "available", Status: "active", Attributes: []byte(`{}`), ReorderPoint: decimal.NewFromInt(10)})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestEOQRejectsForeignAndMissingVariants(t *testing.T) {
	f := setup(t)
	b := f.business(t, "draft")
	other := f.business(t, "draft")
	f.member(t, b, "owner")
	own := f.variant(t, b)
	foreign := f.variant(t, other)
	base := "/analytics/eoq?business_id=" + b.ID.String() + "&variant_id="
	got := decode[map[string]any](t, f.request(t, "GET", base+own.ID.String(), "", 200))
	if got["demand"] != "0" || got["estimated_eoq"] != "0.000" || got["order_cost"] != "100" || got["holding_cost_rate"] != "0.2" || got["period_days"] != float64(90) {
		t.Fatalf("EOQ contract %+v", got)
	}
	foreignBody := f.request(t, "GET", base+foreign.ID.String(), "", 404)
	missingBody := f.request(t, "GET", base+uuid.NewString(), "", 404)
	if string(foreignBody) != string(missingBody) {
		t.Fatal("foreign resource existence disclosed")
	}
}
func TestLowStockExcludesForeignTenantAndLocationRows(t *testing.T) {
	f := setup(t)
	b := f.business(t, "draft")
	other := f.business(t, "draft")
	f.member(t, b, "owner")
	v := f.variant(t, b)
	ctx := context.Background()
	loc, err := f.q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: b.ID, Name: "Own", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := f.q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: other.ID, Name: "Foreign", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	// Adversarial legacy rows are possible with the schema's single-column FKs.
	// The read boundary must not disclose a foreign tenant's stock or location.
	for _, row := range []struct {
		business, location uuid.UUID
		quantity           string
	}{{b.ID, loc.ID, "5"}, {other.ID, foreign.ID, "-123"}, {b.ID, foreign.ID, "-456"}} {
		if _, err = f.pool.Exec(ctx, "INSERT INTO stock_levels(business_id,variant_id,location_id,quantity_on_hand) VALUES($1,$2,$3,$4)", row.business, v.ID, row.location, row.quantity); err != nil {
			t.Fatal(err)
		}
	}
	data := f.request(t, "GET", "/analytics/low-stock?business_id="+b.ID.String(), "", 200)
	rows := decode[[]analytics.LowStockResponse](t, data)
	if len(rows) != 1 || rows[0].QuantityOnHand != "5" {
		t.Fatalf("foreign stock exposed: %s", data)
	}
}
func TestAnalyticsPrivateRoleAndPaginationContracts(t *testing.T) {
	f := setup(t)
	b := f.business(t, "draft")
	f.member(t, b, "owner")
	for _, path := range []string{"/analytics/abc", "/analytics/pareto", "/analytics/low-stock"} {
		data := f.request(t, "GET", path+"?business_id="+b.ID.String(), "", 200)
		if string(data) != "[]" {
			t.Fatalf("empty analytics must be []: %s", data)
		}
		f.request(t, "GET", path+"?business_id="+b.ID.String()+"&limit=101", "", 400)
		f.identity.Role = "staff"
		f.request(t, "GET", path+"?business_id="+b.ID.String(), "", 403)
		f.identity.Role = "owner"
		f.request(t, "GET", path+"?business_id="+uuid.NewString(), "", 403)
	}
}
