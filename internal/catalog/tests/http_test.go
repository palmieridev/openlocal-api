package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/catalog"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type fixture struct {
	pool            *pgxpool.Pool
	q               *db.Queries
	app             *fiber.App
	business, other db.Business
	identity        auth.Context
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("OPENLOCAL_CATALOG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set OPENLOCAL_CATALOG_TEST_DATABASE_URL to a migrated disposable local database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.ConnConfig.Database, "openlocal_catalog_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("catalog tests require a disposable local openlocal_catalog_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	f := &fixture{pool: pool, q: db.New(pool)}
	for _, target := range []*db.Business{&f.business, &f.other} {
		*target, err = f.q.CreateBusiness(ctx, db.CreateBusinessParams{Name: "Catalog Test", Slug: "catalog-" + uuid.NewString(), BusinessType: "retail", Status: "active", Timezone: "America/Mexico_City", LocationMode: "fixed"})
		if err != nil {
			t.Fatal(err)
		}
		id := target.ID
		t.Cleanup(func() {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM businesses WHERE id=$1", id); err != nil {
				t.Error(err)
			}
		})
	}
	f.identity = auth.Context{ClerkUserID: "user_catalog_" + uuid.NewString(), ClerkOrgID: "org_catalog_" + uuid.NewString(), Role: "owner"}
	user, err := f.q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: f.identity.ClerkUserID})
	if err != nil {
		t.Fatal(err)
	}
	f.identity.UserID = user.ID
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", user.ID)
		if err != nil {
			t.Error(err)
		}
	})
	_, err = f.q.AddBusinessMember(ctx, db.AddBusinessMemberParams{BusinessID: f.business.ID, UserID: user.ID, ClerkOrgID: f.identity.ClerkOrgID, Role: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	f.app = fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	h := catalog.NewHandler(api.NewRuntime(nil, pool, nil))
	h.RegisterPublicRoutes(f.app)
	private := f.app.Group("/private", func(c *fiber.Ctx) error { auth.SetFiber(c, f.identity); return c.Next() })
	h.RegisterPrivateRoutes(private)
	return f
}

func (f *fixture) request(t *testing.T, method, path, body string, status int) []byte {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := f.app.Test(req, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, res.StatusCode, status, data)
	}
	return data
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func (f *fixture) product(t *testing.T) catalog.ProductResponse {
	t.Helper()
	return decode[catalog.ProductResponse](t, f.request(t, "POST", "/private/products", fmt.Sprintf(`{"business_id":%q,"name":"Test Coffee","status":"active","is_public":true}`, f.business.ID), 201))
}

func (f *fixture) variantBody(productID uuid.UUID, sku string) string {
	return fmt.Sprintf(`{"business_id":%q,"product_id":%q,"sku":%q,"barcode":%q,"price":"12.50","cost":"5.00","image_url":"https://example.com/first.jpg","track_inventory":true}`, f.business.ID, productID, sku, "barcode-"+sku)
}

func TestCatalogHTTPCharacterization(t *testing.T) {
	f := setup(t)
	p := f.product(t)
	v := decode[catalog.VariantResponse](t, f.request(t, "POST", "/private/variants", f.variantBody(p.ID, "coffee-1"), 201))
	if v.SKU != "COFFEE-1" || v.Name != "" || v.InternalCode != "COFFEE-1" || v.Price != "12.50" || v.IsPublic == nil || !*v.IsPublic {
		t.Fatalf("unexpected variant: %+v", v)
	}
	for _, path := range []string{"/variants/" + v.ID.String(), "/variants/by-sku/coffee-1", "/variants/by-barcode/barcode-coffee-1"} {
		got := decode[catalog.VariantResponse](t, f.request(t, "GET", "/private"+path+"?business_id="+f.business.ID.String(), "", 200))
		if got.ID != v.ID || got.ImageURL == nil || *got.ImageURL != "https://example.com/first.jpg" {
			t.Fatalf("read mismatch: %+v", got)
		}
	}
	update := strings.ReplaceAll(f.variantBody(p.ID, "COFFEE-1"), `,"image_url":"https://example.com/first.jpg"`, "")
	update = strings.Replace(update, `"price":"12.50"`, `"price":"13.00","is_public":false`, 1)
	v = decode[catalog.VariantResponse](t, f.request(t, "PATCH", "/private/variants/"+v.ID.String(), update, 200))
	if v.ImageURL == nil || *v.ImageURL != "https://example.com/first.jpg" || v.IsPublic == nil || *v.IsPublic {
		t.Fatalf("omission/visibility: %+v", v)
	}
	update = strings.Replace(update, `,"is_public":false`, "", 1)
	v = decode[catalog.VariantResponse](t, f.request(t, "PATCH", "/private/variants/"+v.ID.String(), update, 200))
	if v.IsPublic == nil || *v.IsPublic {
		t.Fatal("omitted visibility overwrote hidden value")
	}
	f.request(t, "GET", "/public/businesses/"+f.business.Slug+"/products", "", 200)
	updated := decode[catalog.ProductResponse](t, f.request(t, "PATCH", "/private/products/"+p.ID.String(), fmt.Sprintf(`{"business_id":%q,"name":"Renamed Coffee","slug":"ignored-change","status":"active","is_public":true}`, f.business.ID), 200))
	if updated.Slug != p.Slug {
		t.Fatal("product slug changed")
	}
	f.request(t, "DELETE", "/private/variants/"+v.ID.String()+"?business_id="+f.business.ID.String(), "", 204)
	f.request(t, "GET", "/private/variants/by-sku/COFFEE-1?business_id="+f.business.ID.String(), "", 404)
	f.request(t, "DELETE", "/private/products/"+p.ID.String()+"?business_id="+f.business.ID.String(), "", 204)
	archived := decode[catalog.ProductResponse](t, f.request(t, "GET", "/private/products/"+p.ID.String()+"?business_id="+f.business.ID.String(), "", 200))
	if archived.Status != "archived" {
		t.Fatal("product did not archive")
	}
}

func TestCatalogRejectsForeignCategoryWithoutWrites(t *testing.T) {
	f := setup(t)
	var categoryID uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO categories(business_id,name,slug) VALUES($1,'Other','other') RETURNING id`, f.other.ID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{categoryID, uuid.New()} {
		f.request(t, "POST", "/private/products", fmt.Sprintf(`{"business_id":%q,"category_id":%q,"name":"Rejected Product"}`, f.business.ID, id), 404)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM products WHERE business_id=$1", f.business.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected creates left %d products", count)
	}
	p := f.product(t)
	for _, id := range []uuid.UUID{categoryID, uuid.New()} {
		f.request(t, "PATCH", "/private/products/"+p.ID.String(), fmt.Sprintf(`{"business_id":%q,"category_id":%q,"name":"Rejected Update"}`, f.business.ID, id), 404)
	}
	stored, err := f.q.GetProductForBusiness(context.Background(), db.GetProductForBusinessParams{ID: p.ID, BusinessID: f.business.ID})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != p.Name || stored.CategoryID.Valid {
		t.Fatalf("rejected update changed product: %+v", stored)
	}
}

func TestCatalogResourceOwnershipAndImmutableRelationships(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := f.product(t)
	otherProduct, err := f.q.CreateProduct(ctx, db.CreateProductParams{BusinessID: f.other.ID, Name: "Other Product", Slug: "other", Unit: "piece", ProductType: "stocked_product", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	v := decode[catalog.VariantResponse](t, f.request(t, "POST", "/private/variants", f.variantBody(p.ID, "OWNED"), 201))
	// Owned tenant, foreign/nonexistent referenced product: indistinguishable
	// rejections with neither variant nor image writes.
	for _, id := range []uuid.UUID{otherProduct.ID, uuid.New()} {
		f.request(t, "POST", "/private/variants", f.variantBody(id, "REJECTED"), 404)
		f.request(t, "PATCH", "/private/variants/"+v.ID.String(), f.variantBody(id, "REJECTED"), 404)
		f.request(t, "GET", "/private/products/"+id.String()+"?business_id="+f.business.ID.String(), "", 404)
		f.request(t, "GET", "/private/products/"+id.String()+"/variants?business_id="+f.business.ID.String(), "", 404)
		f.request(t, "PATCH", "/private/products/"+id.String(), fmt.Sprintf(`{"business_id":%q,"name":"Rejected"}`, f.business.ID), 404)
		// Existing archive contract: unknown/foreign resources are idempotent no-ops.
		f.request(t, "DELETE", "/private/products/"+id.String()+"?business_id="+f.business.ID.String(), "", 204)
	}
	second, err := f.q.CreateProduct(ctx, db.CreateProductParams{BusinessID: f.business.ID, Name: "Second Product", Slug: "second", Unit: "piece", ProductType: "stocked_product", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, "PATCH", "/private/variants/"+v.ID.String(), f.variantBody(second.ID, "REJECTED"), 404)
	// A foreign business may use the same SKU/barcode. Reads must resolve only
	// within the authorized business, including when the other tenant has a hit.
	params, _, err := catalog.VariantParams(catalog.VariantRequest{BusinessID: f.other.ID.String(), ProductID: otherProduct.ID.String(), SKU: "OTHER", Barcode: ptr("other-barcode"), Price: "9.00"})
	if err != nil {
		t.Fatal(err)
	}
	otherVariant, err := f.q.CreateVariant(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	params.Sku = v.SKU
	params.InternalCode = v.InternalCode
	params.Barcode.String = "barcode-OWNED"
	if _, err := f.q.CreateVariant(ctx, params); err != nil {
		t.Fatalf("same SKU/barcode should be allowed in a different business: %v", err)
	}
	for _, path := range []string{"/variants/by-sku/OWNED", "/variants/by-barcode/barcode-OWNED"} {
		got := decode[catalog.VariantResponse](t, f.request(t, "GET", "/private"+path+"?business_id="+f.business.ID.String(), "", 200))
		if got.ID != v.ID {
			t.Fatalf("lookup crossed tenant boundary: %+v", got)
		}
	}
	for _, id := range []uuid.UUID{otherVariant.ID, uuid.New()} {
		f.request(t, "GET", "/private/variants/"+id.String()+"?business_id="+f.business.ID.String(), "", 404)
		f.request(t, "PATCH", "/private/variants/"+id.String(), f.variantBody(p.ID, "REJECTED"), 404)
		f.request(t, "DELETE", "/private/variants/"+id.String()+"?business_id="+f.business.ID.String(), "", 204)
	}
	f.request(t, "GET", "/private/variants/by-sku/OTHER?business_id="+f.business.ID.String(), "", 404)
	f.request(t, "GET", "/private/variants/by-barcode/other-barcode?business_id="+f.business.ID.String(), "", 404)
	stored, err := f.q.GetVariantForBusiness(ctx, db.GetVariantForBusinessParams{ID: v.ID, BusinessID: f.business.ID})
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProductVariant.Sku != "OWNED" || stored.ProductVariant.ProductID != p.ID || stored.ImageUrl != "https://example.com/first.jpg" {
		t.Fatalf("rejection changed variant: %+v", stored)
	}
	foreign, err := f.q.GetVariantForBusiness(ctx, db.GetVariantForBusinessParams{ID: otherVariant.ID, BusinessID: f.other.ID})
	if err != nil || foreign.ProductVariant.Status != "active" {
		t.Fatalf("foreign archive changed state: %+v, %v", foreign, err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM product_variants WHERE business_id=$1", f.business.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rejections left %d variants", count)
	}
	product, err := f.q.GetProductForBusiness(ctx, db.GetProductForBusinessParams{ID: otherProduct.ID, BusinessID: f.other.ID})
	if err != nil || product.Status != "active" {
		t.Fatalf("foreign product changed: %+v, %v", product, err)
	}
}

func TestCatalogPublicVisibilityAndProjection(t *testing.T) {
	f := setup(t)
	p := f.product(t)
	v := decode[catalog.VariantResponse](t, f.request(t, "POST", "/private/variants", f.variantBody(p.ID, "PUBLIC"), 201))
	path := "/public/businesses/" + f.business.Slug + "/products"
	rows := decode[[]map[string]any](t, f.request(t, "GET", path, "", 200))
	if len(rows) != 1 || rows[0]["variant_id"] != v.ID.String() || rows[0]["price"] != "12.50" {
		t.Fatalf("public rows: %+v", rows)
	}
	allowed := strings.Fields("id name slug description brand unit product_type variant_id sku variant_name variant_description price_note price currency public_stock_status image_url")
	if len(rows[0]) != len(allowed) {
		t.Fatalf("public shape changed: %+v", rows[0])
	}
	for _, key := range allowed {
		if _, ok := rows[0][key]; !ok {
			t.Fatalf("missing public field %s", key)
		}
	}
	for _, key := range []string{"cost", "business_id", "barcode", "internal_code", "attributes", "track_inventory", "quantity_on_hand", "reorder_point", "lead_time_days", "created_at", "clerk_org_id", "stock_movements", "supplier_notes"} {
		if _, ok := rows[0][key]; ok {
			t.Fatalf("public field leak: %s", key)
		}
	}
	ctx := context.Background()
	for _, change := range []struct{ hide, restore string }{
		{"UPDATE product_variants SET is_public=false WHERE id=$1", "UPDATE product_variants SET is_public=true WHERE id=$1"},
		{"UPDATE product_variants SET status='archived' WHERE id=$1", "UPDATE product_variants SET status='active' WHERE id=$1"},
		{"UPDATE products SET is_public=false WHERE id=$1", "UPDATE products SET is_public=true WHERE id=$1"},
		{"UPDATE products SET status='draft' WHERE id=$1", "UPDATE products SET status='active' WHERE id=$1"},
		{"UPDATE businesses SET status='suspended' WHERE id=$1", "UPDATE businesses SET status='active' WHERE id=$1"},
	} {
		id := v.ID
		if strings.HasPrefix(change.hide, "UPDATE products ") {
			id = p.ID
		}
		if strings.HasPrefix(change.hide, "UPDATE businesses ") {
			id = f.business.ID
		}
		if _, err := f.pool.Exec(ctx, change.hide, id); err != nil {
			t.Fatal(err)
		}
		if body := string(f.request(t, "GET", path, "", 200)); body != "[]" {
			t.Fatalf("hidden row returned: %s", body)
		}
		if _, err := f.pool.Exec(ctx, change.restore, id); err != nil {
			t.Fatal(err)
		}
	}
	f.request(t, "GET", path+"?limit=1&offset=1", "", 200)
	if body := string(f.request(t, "GET", "/public/businesses/missing/products", "", 200)); body != "[]" {
		t.Fatalf("empty list: %s", body)
	}
}

func TestCatalogAuthorizationAndMalformedInput(t *testing.T) {
	f := setup(t)
	p := f.product(t)
	for _, body := range []string{`{"unknown":true}`, `{}`, `{"business_id":"bad","name":"Coffee"}`, `[]`, `{} {}`} {
		f.request(t, "POST", "/private/products", body, 400)
	}
	for _, query := range []string{"limit=101", "limit=0", "limit=-1", "limit=oops", "offset=-1", "offset=2000000001", "offset=2147483648"} {
		f.request(t, "GET", "/public/businesses/"+f.business.Slug+"/products?"+query, "", 400)
		f.request(t, "GET", "/private/products?business_id="+f.business.ID.String()+"&"+query, "", 400)
		f.request(t, "GET", "/private/products/"+p.ID.String()+"/variants?business_id="+f.business.ID.String()+"&"+query, "", 400)
	}
	for _, bad := range []string{"-1", "1.001", "10000000000", "1e1000"} {
		body := strings.Replace(f.variantBody(p.ID, "INVALID"), `"price":"12.50"`, `"price":"`+bad+`"`, 1)
		f.request(t, "POST", "/private/variants", body, 400)
	}
	f.request(t, "GET", "/private/products?business_id="+f.other.ID.String(), "", 403)
	f.request(t, "POST", "/private/products", fmt.Sprintf(`{"business_id":%q,"name":"Forbidden"}`, f.other.ID), 403)
	f.identity.Role = "staff"
	f.request(t, "GET", "/private/products?business_id="+f.business.ID.String(), "", 200)
	f.request(t, "POST", "/private/variants", f.variantBody(p.ID, "FORBIDDEN"), 403)
	f.request(t, "PATCH", "/private/products/"+p.ID.String(), fmt.Sprintf(`{"business_id":%q,"name":"Forbidden"}`, f.business.ID), 403)
	f.request(t, "DELETE", "/private/products/"+p.ID.String()+"?business_id="+f.business.ID.String(), "", 403)
	f.identity.ClerkOrgID = "org_wrong"
	f.request(t, "GET", "/private/products?business_id="+f.business.ID.String(), "", 403)
	f.identity = auth.Context{}
	f.request(t, "GET", "/private/products?business_id="+f.business.ID.String(), "", 401)
}

func TestCatalogUniquenessRollsBack(t *testing.T) {
	f := setup(t)
	p := f.product(t)
	v := decode[catalog.VariantResponse](t, f.request(t, "POST", "/private/variants", f.variantBody(p.ID, "FIRST"), 201))
	second := decode[catalog.VariantResponse](t, f.request(t, "POST", "/private/variants", f.variantBody(p.ID, "SECOND"), 201))
	// Constraint errors retain the existing generic HTTP error contract.
	f.request(t, "POST", "/private/variants", f.variantBody(p.ID, "FIRST"), 500)
	f.request(t, "PATCH", "/private/variants/"+second.ID.String(), f.variantBody(p.ID, "FIRST"), 500)
	barcodeDuplicate := strings.Replace(f.variantBody(p.ID, "THIRD"), "barcode-THIRD", "barcode-FIRST", 1)
	f.request(t, "POST", "/private/variants", barcodeDuplicate, 500)
	stored, err := f.q.GetVariantForBusiness(context.Background(), db.GetVariantForBusinessParams{ID: second.ID, BusinessID: f.business.ID})
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProductVariant.Sku != "SECOND" || stored.ImageUrl != "https://example.com/first.jpg" {
		t.Fatalf("uniqueness error changed variant: %+v", stored)
	}
	params, _, err := catalog.VariantParams(catalog.VariantRequest{BusinessID: f.other.ID.String(), ProductID: p.ID.String(), SKU: v.SKU, Price: "1.00"})
	if err != nil {
		t.Fatal(err)
	}
	// The SQL boundary itself rejects cross-tenant parent references.
	if _, err = f.q.CreateVariant(context.Background(), params); err == nil {
		t.Fatal("SQL accepted a foreign product")
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM product_variants WHERE business_id=$1", f.business.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("duplicate requests left %d variants", count)
	}
}

func ptr[T any](value T) *T { return &value }
