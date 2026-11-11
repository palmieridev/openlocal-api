package marketplace_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/marketplace"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func TestPublicBusinessProjectionsPreserveHoursAndHideIdentity(t *testing.T) {
	f := setup(t)
	b := f.business(t, "active")
	empty := f.business(t, "active")
	draft := f.business(t, "draft")
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, "UPDATE businesses SET clerk_org_id=$2 WHERE id=$1", b.ID, "secret-org-"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	for _, hour := range []db.UpsertBusinessHourParams{{BusinessID: b.ID, DayOfWeek: 0, OpensAt: pgtype.Time{Microseconds: 22 * 60 * 60 * 1000000, Valid: true}, ClosesAt: pgtype.Time{Microseconds: 2 * 60 * 60 * 1000000, Valid: true}}, {BusinessID: b.ID, DayOfWeek: 6, IsClosed: true}} {
		if _, err := f.q.UpsertBusinessHour(ctx, hour); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/public/businesses/" + b.Slug, "/marketplace/businesses"} {
		data := f.request(t, "GET", path, "", 200)
		var items []map[string]any
		if strings.Contains(path, "/public/") {
			items = []map[string]any{decode[map[string]any](t, data)}
		} else {
			items = decode[[]map[string]any](t, data)
		}
		found := false
		for _, item := range items {
			for _, key := range []string{"clerk_org_id", "status", "is_open", "open_now", "cost", "supplier_notes"} {
				if _, ok := item[key]; ok {
					t.Fatalf("public key %s exposed", key)
				}
			}
			if item["id"] == draft.ID.String() {
				t.Fatal("draft storefront exposed")
			}
			if item["id"] == empty.ID.String() {
				if h, ok := item["hours"].([]any); !ok || len(h) != 0 {
					t.Fatalf("missing hours must be []: %s", data)
				}
			}
			if item["id"] != b.ID.String() {
				continue
			}
			found = true
			if item["timezone"] != "America/Tijuana" {
				t.Fatal("business timezone lost")
			}
			hours, ok := item["hours"].([]any)
			if !ok || len(hours) != 2 {
				t.Fatalf("hours %s", data)
			}
			monday := hours[0].(map[string]any)
			sunday := hours[1].(map[string]any)
			if monday["day_of_week"] != float64(0) || monday["opens_at"] != "22:00" || monday["closes_at"] != "02:00" || sunday["day_of_week"] != float64(6) || sunday["opens_at"] != nil || sunday["closes_at"] != nil || sunday["is_closed"] != true {
				t.Fatalf("hours %s", data)
			}
		}
		if !found {
			t.Fatal("active business missing")
		}
	}
	f.request(t, "GET", "/public/businesses/"+draft.Slug, "", 404)
	f.request(t, "GET", "/public/businesses/missing-"+uuid.NewString(), "", 404)
}
func TestMarketplaceProductsOnlyExposePublicVariantData(t *testing.T) {
	f := setup(t)
	b := f.business(t, "active")
	ctx := context.Background()
	marker := "public-product-" + uuid.NewString()
	p, err := f.q.CreateProduct(ctx, db.CreateProductParams{BusinessID: b.ID, Name: marker, Slug: marker, Unit: "piece", ProductType: "stocked_product", IsPublic: true, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.q.CreateVariant(ctx, db.CreateVariantParams{BusinessID: b.ID, ProductID: p.ID, Sku: "PUBLIC", InternalCode: "PRIVATE-CODE", Name: "Visible", Description: sql.NullString{String: "Variant detail", Valid: true}, PriceNote: sql.NullString{String: "Quoted price", Valid: true}, Price: decimal.RequireFromString("12.50"), Cost: decimal.NullDecimal{Decimal: decimal.NewFromInt(7), Valid: true}, Currency: "MXN", PublicStockStatus: "available", TrackInventory: true, IsPublic: true, Status: "active", Attributes: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/marketplace/products", "/marketplace/search"} {
		data := f.request(t, "GET", path+"?q="+marker, "", 200)
		rows := decode[[]marketplace.ProductResponse](t, data)
		if len(rows) != 1 || rows[0].VariantID != v.ID || rows[0].Price != "12.50" || rows[0].VariantDescription == nil || *rows[0].VariantDescription != "Variant detail" || rows[0].PriceNote == nil || *rows[0].PriceNote != "Quoted price" {
			t.Fatalf("projection %s", data)
		}
		raw := decode[[]map[string]any](t, data)
		for _, key := range []string{"cost", "internal_code", "quantity_on_hand", "stock_movements", "clerk_org_id", "supplier_id", "private_notes", "attributes"} {
			if _, ok := raw[0][key]; ok {
				t.Fatalf("private field %s exposed", key)
			}
		}
	}
	if _, err = f.pool.Exec(ctx, "UPDATE product_variants SET is_public=false WHERE id=$1", v.ID); err != nil {
		t.Fatal(err)
	}
	got := decode[[]marketplace.ProductResponse](t, f.request(t, "GET", "/marketplace/products?q="+marker, "", 200))
	if len(got) != 0 {
		t.Fatal("private variant exposed")
	}
}
func TestMarketplaceRejectsInvalidFilters(t *testing.T) {
	f := setup(t)
	for _, query := range []string{"limit=101", "min_lat=91", "min_lng=-181", "min_lat=30&max_lat=20", "country=XYZ", "min_lat=invalid"} {
		f.request(t, "GET", "/marketplace/businesses?"+query, "", 400)
	}
	f.request(t, "GET", fmt.Sprintf("/marketplace/products?q=%s", strings.Repeat("x", 201)), "", 400)
}
