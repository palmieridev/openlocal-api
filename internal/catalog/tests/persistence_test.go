package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/catalog"
	"github.com/palmieridev/openlocal-api/internal/catalog/catalogpostgres"
	"github.com/palmieridev/openlocal-api/internal/catalog/management"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func (f *fixture) service() *management.Service {
	store := catalogpostgres.New(f.pool, f.q)
	return management.New(store, store, store)
}

func (f *fixture) actor() management.Identity {
	return management.Identity{BusinessID: f.business.ID, ActorID: f.identity.UserID}
}

func TestCatalogPersistenceRejectsForeignCategory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var categoryID uuid.UUID
	if err := f.pool.QueryRow(ctx, "INSERT INTO categories(business_id,name,slug) VALUES($1,'Other','other') RETURNING id", f.other.ID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	store := catalogpostgres.New(f.pool, f.q)
	data := management.ProductData{Name: "Scoped Product", Slug: "scoped-product", Unit: "piece", ProductType: "stocked_product", Status: "active"}
	p, err := store.CreateProduct(ctx, f.business.ID, data)
	if err != nil {
		t.Fatal(err)
	}
	data.CategoryID = &categoryID
	data.Slug = "rejected-product"
	if _, err := store.CreateProduct(ctx, f.business.ID, data); !errors.Is(err, management.ErrNotFound) {
		t.Fatalf("SQL create category guard: %v", err)
	}
	data.Name = "Rejected"
	if _, err := store.UpdateProduct(ctx, f.business.ID, p.ID, data); !errors.Is(err, management.ErrNotFound) {
		t.Fatalf("SQL update category guard: %v", err)
	}
	stored, err := store.Product(ctx, f.business.ID, p.ID)
	if err != nil || stored.Name != p.Name || stored.CategoryID != nil {
		t.Fatalf("rejection changed product: %+v, %v", stored, err)
	}
}

func TestCatalogVariantAndImageRollbackTogether(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := f.product(t)
	service := f.service()
	input := management.VariantInput{ProductID: p.ID.String(), SKU: "ORIGINAL", Price: "12.50", ImageURL: ptr("https://example.com/original.jpg")}
	v, err := service.CreateVariant(ctx, f.actor(), input)
	if err != nil {
		t.Fatal(err)
	}
	// Static DDL and a fixture marker keep fault injection scoped without
	// interpolating identifiers or data into SQL.
	if _, err := f.pool.Exec(ctx, "UPDATE businesses SET description=$2 WHERE id=$1", f.business.ID, "catalog-test-reject-image"); err != nil {
		t.Fatal(err)
	}
	failURL := "https://example.com/catalog-fixture-image-failure.jpg"
	ddl := `CREATE FUNCTION reject_catalog_test_image() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.url = 'https://example.com/catalog-fixture-image-failure.jpg' AND EXISTS (
   SELECT 1 FROM product_variants pv JOIN businesses b ON b.id = pv.business_id
   WHERE pv.id = NEW.variant_id AND b.description = 'catalog-test-reject-image'
 ) THEN RAISE EXCEPTION 'catalog fixture image failure'; END IF;
 RETURN NEW;
END $$`
	if _, err := f.pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := f.pool.Exec(cleanupCtx, "DROP FUNCTION reject_catalog_test_image() CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if _, err := f.pool.Exec(ctx, "CREATE TRIGGER reject_catalog_test_image BEFORE INSERT ON product_images FOR EACH ROW EXECUTE FUNCTION reject_catalog_test_image()"); err != nil {
		t.Fatal(err)
	}
	input.SKU = "FAILED-CREATE"
	input.ImageURL = &failURL
	if _, err := service.CreateVariant(ctx, f.actor(), input); err == nil {
		t.Fatal("expected create image failure")
	}
	input.SKU = "FAILED-UPDATE"
	input.Price = "99.00"
	if _, err := service.UpdateVariant(ctx, f.actor(), v.ID, input); err == nil {
		t.Fatal("expected update image failure")
	}
	stored, err := service.Variant(ctx, f.actor(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SKU != v.SKU || !stored.Price.Equal(v.Price) || !stored.UpdatedAt.Equal(v.UpdatedAt) || stored.ImageURL == nil || *stored.ImageURL != *v.ImageURL {
		t.Fatalf("failed image write partially changed variant: %+v", stored)
	}
	var variants, images int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM product_variants WHERE business_id=$1", f.business.ID).Scan(&variants); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM product_images WHERE variant_id=$1", v.ID).Scan(&images); err != nil {
		t.Fatal(err)
	}
	if variants != 1 || images != 1 {
		t.Fatalf("failed writes left variants=%d images=%d", variants, images)
	}
	input.SKU = v.SKU
	input.Price = "13.00"
	input.ImageURL = ptr("https://example.com/replaced.jpg")
	v, err = service.UpdateVariant(ctx, f.actor(), v.ID, input)
	if err != nil || v.ImageURL == nil || *v.ImageURL != *input.ImageURL {
		t.Fatalf("replace: %+v, %v", v, err)
	}
	input.ImageURL = ptr("")
	v, err = service.UpdateVariant(ctx, f.actor(), v.ID, input)
	if err != nil || v.ImageURL != nil {
		t.Fatalf("clear: %+v, %v", v, err)
	}
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM product_images WHERE variant_id=$1", v.ID).Scan(&images); err != nil || images != 0 {
		t.Fatalf("clear left images=%d: %v", images, err)
	}
}

func TestCatalogConcurrentImageReplacement(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := f.product(t)
	input := management.VariantInput{ProductID: p.ID.String(), SKU: "CONCURRENT", Price: "12.50", ImageURL: ptr("https://example.com/initial.jpg")}
	v, err := f.service().CreateVariant(ctx, f.actor(), input)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, "SELECT id FROM product_variants WHERE id=$1 FOR UPDATE", v.ID); err != nil {
		t.Fatal(err)
	}
	applicationName := "catalog_concurrency_" + uuid.NewString()
	cfg := f.pool.Config()
	cfg.ConnConfig.RuntimeParams["application_name"] = applicationName
	workers, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer workers.Close()
	store := catalogpostgres.New(workers, db.New(workers))
	service := management.New(store, store, store)
	results := make(chan error, 2)
	for _, url := range []string{"https://example.com/first.jpg", "https://example.com/second.jpg"} {
		update := input
		update.ImageURL = ptr(url)
		go func() { _, err := service.UpdateVariant(ctx, f.actor(), v.ID, update); results <- err }()
	}
	// Observe both operations waiting on this fixture's held row lock before
	// releasing it. A bounded database observation proves actual overlap.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		err := f.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock'", applicationName).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("updates did not reach overlapping row locks")
		case <-ticker.C:
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("updates did not finish")
		}
	}
	var count int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM product_images WHERE variant_id=$1", v.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent replacement left %d images", count)
	}
	stored, err := f.service().Variant(ctx, f.actor(), v.ID)
	if err != nil || stored.ImageURL == nil || (*stored.ImageURL != "https://example.com/first.jpg" && *stored.ImageURL != "https://example.com/second.jpg") {
		t.Fatalf("concurrent result: %+v, %v", stored, err)
	}
}

func TestCatalogPublicReaderRejectsLegacyCrossBusinessVariant(t *testing.T) {
	f := setup(t)
	p := f.product(t)
	ctx := context.Background()
	// Characterize a corrupted legacy row that predates the SQL ownership guard.
	// The public join must not project another business's variant onto this store.
	if _, err := f.pool.Exec(ctx, `INSERT INTO product_variants(product_id,business_id,sku,internal_code,price,currency,status,is_public)
 VALUES($1,$2,'FOREIGN','FOREIGN',5,'MXN','active',true)`, p.ID, f.other.ID); err != nil {
		t.Fatal(err)
	}
	if body := string(f.request(t, "GET", "/public/businesses/"+f.business.Slug+"/products", "", 200)); body != "[]" {
		t.Fatalf("legacy cross-business row leaked: %s", body)
	}
	rows, err := f.q.SearchMarketplaceProducts(ctx, db.SearchMarketplaceProductsParams{SearchQuery: "Test Coffee", LimitCount: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == p.ID {
			t.Fatal("legacy cross-business variant leaked through marketplace SQL")
		}
	}
}

func TestCatalogRoutePaginationContract(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	service := f.service()
	for i := 1; i <= 27; i++ {
		product, err := service.CreateProduct(ctx, f.actor(), management.ProductInput{Name: fmt.Sprintf("Product %02d", i), IsPublic: true, Status: "active"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.CreateVariant(ctx, f.actor(), management.VariantInput{ProductID: product.ID.String(), SKU: fmt.Sprintf("PAGE-%02d", i), Price: "1.00"})
		if err != nil {
			t.Fatal(err)
		}
	}
	private := "/private/products?business_id=" + f.business.ID.String()
	public := "/public/businesses/" + f.business.Slug + "/products"
	for _, tc := range []struct {
		privateSuffix, publicSuffix string
		want                        int
	}{
		{"", "", 25},
		{"&limit=100", "?limit=100", 27},
		{"&limit=2&offset=25", "?limit=2&offset=25", 2},
		{"&offset=27", "?offset=27", 0},
	} {
		products := decode[[]catalog.ProductResponse](t, f.request(t, "GET", private+tc.privateSuffix, "", 200))
		cards := decode[[]map[string]any](t, f.request(t, "GET", public+tc.publicSuffix, "", 200))
		if len(products) != tc.want || len(cards) != tc.want {
			t.Fatalf("pagination %q/%q: private=%d public=%d, want %d", tc.privateSuffix, tc.publicSuffix, len(products), len(cards), tc.want)
		}
		if tc.want == 2 && (cards[0]["name"] != "Product 26" || cards[1]["name"] != "Product 27") {
			t.Fatalf("offset/order: %+v", cards)
		}
	}
}

func TestCatalogVariantListPaginationContract(t *testing.T) {
	f := setup(t)
	p := f.product(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	service := f.service()
	for i := 1; i <= 27; i++ {
		_, err := service.CreateVariant(ctx, f.actor(), management.VariantInput{ProductID: p.ID.String(), SKU: fmt.Sprintf("PAGE-%02d", i), Price: "1.00"})
		if err != nil {
			t.Fatal(err)
		}
	}
	path := "/private/products/" + p.ID.String() + "/variants?business_id=" + f.business.ID.String()
	for _, tc := range []struct {
		query string
		want  int
	}{{"", 25}, {"&limit=100", 27}, {"&limit=2&offset=25", 2}, {"&offset=27", 0}} {
		rows := decode[[]catalog.VariantResponse](t, f.request(t, "GET", path+tc.query, "", 200))
		if len(rows) != tc.want {
			t.Fatalf("variant page %q: got %d, want %d", tc.query, len(rows), tc.want)
		}
		if tc.want == 2 && (rows[0].SKU != "PAGE-26" || rows[1].SKU != "PAGE-27") {
			t.Fatalf("variant offset/order: %+v", rows)
		}
	}
}
