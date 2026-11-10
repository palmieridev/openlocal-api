package catalog_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/catalog/management"
)

type productMethods interface{ management.Products }

type productProbe struct {
	productMethods
	categoryBusiness, category uuid.UUID
	categoryErr                error
	writes                     int
	data                       management.ProductData
}

func (p *productProbe) RequireCategory(_ context.Context, businessID, id uuid.UUID) error {
	p.categoryBusiness, p.category = businessID, id
	return p.categoryErr
}

func (p *productProbe) CreateProduct(_ context.Context, businessID uuid.UUID, data management.ProductData) (management.Product, error) {
	p.writes++
	p.data = data
	return management.Product{BusinessID: businessID, ProductData: data}, nil
}

func TestCatalogServiceNormalizesProductAndScopesCategory(t *testing.T) {
	ctx := context.Background()
	identity := management.Identity{BusinessID: uuid.New(), ActorID: uuid.New()}
	category := uuid.New()
	p := &productProbe{}
	s := management.New(p, nil, nil)
	input := management.ProductInput{Name: "  Test   Coffee  ", CategoryID: ptr(category.String()), Brand: ptr("  Brand  ")}
	result, err := s.CreateProduct(ctx, identity, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.BusinessID != identity.BusinessID || p.categoryBusiness != identity.BusinessID || p.category != category || result.Name != "Test Coffee" || result.Slug != "test-coffee" || result.Unit != "piece" || result.Status != "draft" || result.ProductType != "stocked_product" || result.Brand == nil || *result.Brand != "Brand" {
		t.Fatalf("normalization/scope: %+v, %+v", result, p)
	}
	p.categoryErr = management.ErrNotFound
	_, err = s.CreateProduct(ctx, identity, input)
	if !errors.Is(err, management.ErrNotFound) || p.writes != 1 {
		t.Fatalf("category rejection wrote a product: %v, writes=%d", err, p.writes)
	}
}

type variantMethods interface{ management.Variants }

type variantProbe struct {
	variantMethods
	tx     *variantTxProbe
	begins int
}

func (v *variantProbe) Begin(context.Context) (management.VariantTransaction, error) {
	v.begins++
	return v.tx, nil
}

type variantTxProbe struct {
	management.VariantTransaction
	business, product          uuid.UUID
	parentErr, errorOnImage    error
	writes, commits, rollbacks int
	data                       management.VariantData
}

func (v *variantTxProbe) RequireProduct(_ context.Context, businessID, productID uuid.UUID) error {
	v.business = businessID
	v.product = productID
	return v.parentErr
}

func (v *variantTxProbe) CreateVariant(_ context.Context, businessID uuid.UUID, data management.VariantData) (management.Variant, error) {
	v.writes++
	v.data = data
	return management.Variant{ID: uuid.New(), BusinessID: businessID, VariantData: data}, nil
}

func (v *variantTxProbe) ApplyImage(_ context.Context, _ uuid.UUID, image *string) (*string, error) {
	return image, v.errorOnImage
}

func (v *variantTxProbe) Commit(context.Context) error { v.commits++; return nil }

func (v *variantTxProbe) Rollback(context.Context) error { v.rollbacks++; return nil }

func TestCatalogServiceVariantValidationPrecedesPersistence(t *testing.T) {
	identity := management.Identity{BusinessID: uuid.New(), ActorID: uuid.New()}
	base := management.VariantInput{ProductID: uuid.NewString(), SKU: " sku-1 ", Price: "12.50"}
	for _, test := range []struct {
		name   string
		change func(*management.VariantInput)
	}{
		{"invalid parent", func(v *management.VariantInput) { v.ProductID = "nope" }},
		{"price scale", func(v *management.VariantInput) { v.Price = "1.001" }},
		{"price range", func(v *management.VariantInput) { v.Price = "10000000000" }},
		{"bounded decimal", func(v *management.VariantInput) { v.Price = strings.Repeat("1", 65) }},
		{"negative cost", func(v *management.VariantInput) { v.Cost = ptr("-1") }},
		{"reorder precision", func(v *management.VariantInput) { v.ReorderPoint = "0.0001" }},
		{"currency", func(v *management.VariantInput) { v.Currency = "peso" }},
		{"stock enum", func(v *management.VariantInput) { v.PublicStockStatus = "private" }},
		{"status enum", func(v *management.VariantInput) { v.Status = "draft" }},
		{"lead time", func(v *management.VariantInput) { v.LeadTimeDays = 3651 }},
		{"barcode length", func(v *management.VariantInput) { v.Barcode = ptr(strings.Repeat("b", 129)) }},
		{"image scheme", func(v *management.VariantInput) { v.ImageURL = ptr("javascript:alert(1)") }},
		{"attributes encoding", func(v *management.VariantInput) { v.Attributes = map[string]any{"unsupported": make(chan int)} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &variantProbe{}
			service := management.New(nil, probe, nil)
			input := base
			test.change(&input)
			_, err := service.CreateVariant(context.Background(), identity, input)
			var validation *management.ValidationError
			if !errors.As(err, &validation) || probe.begins != 0 {
				t.Fatalf("invalid input reached persistence: %v, begins=%d", err, probe.begins)
			}
		})
	}
}

func TestCatalogServiceOwnsVariantTransactionAndParentScope(t *testing.T) {
	identity := management.Identity{BusinessID: uuid.New(), ActorID: uuid.New()}
	productID := uuid.New()
	input := management.VariantInput{ProductID: productID.String(), SKU: " sku-1 ", Price: "12.50", Barcode: ptr("  barcode  "), Cost: ptr("  "), Description: ptr("  details  "), ImageURL: ptr(" https://example.com/image.jpg ")}
	for _, name := range []string{"parent rejection", "image failure", "success"} {
		t.Run(name, func(t *testing.T) {
			tx := &variantTxProbe{}
			if name == "parent rejection" {
				tx.parentErr = management.ErrNotFound
			}
			if name == "image failure" {
				tx.errorOnImage = errors.New("image write failed")
			}
			service := management.New(nil, &variantProbe{tx: tx}, nil)
			result, err := service.CreateVariant(context.Background(), identity, input)
			if tx.business != identity.BusinessID || tx.product != productID || tx.rollbacks != 1 {
				t.Fatalf("transaction/scope: %+v", tx)
			}
			if name == "parent rejection" {
				if !errors.Is(err, management.ErrNotFound) || tx.writes != 0 || tx.commits != 0 {
					t.Fatalf("parent failure: %v, %+v", err, tx)
				}
				return
			}
			if name == "image failure" {
				if !errors.Is(err, tx.errorOnImage) || tx.commits != 0 {
					t.Fatalf("image failure: %v, %+v", err, tx)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tx.commits != 1 || tx.writes != 1 || result.SKU != "SKU-1" || result.InternalCode != "SKU-1" || result.IsPublic == nil || !*result.IsPublic || result.Cost != nil || result.Barcode == nil || *result.Barcode != "barcode" || string(result.Attributes) != "{}" || result.ImageURL == nil || *result.ImageURL != "https://example.com/image.jpg" {
				t.Fatalf("result: %+v, transaction: %+v", result, tx)
			}
		})
	}
}

type publicProbe struct {
	slug  string
	page  management.Page
	reads int
}

func (p *publicProbe) PublicProducts(_ context.Context, slug string, page management.Page) ([]management.PublicProduct, error) {
	p.slug = slug
	p.page = page
	p.reads++
	return []management.PublicProduct{}, nil
}

func TestCatalogServicePublicPagination(t *testing.T) {
	p := &publicProbe{}
	s := management.New(nil, nil, p)
	if _, err := s.PublicProducts(context.Background(), "Test Business", management.Page{}); err != nil {
		t.Fatal(err)
	}
	if p.slug != "test-business" || p.page.Limit != 25 || p.page.Offset != 0 {
		t.Fatalf("default page: %+v", p)
	}
	for _, input := range []management.Page{{Limit: 101}, {Limit: -1}, {Offset: -1}, {Offset: 2_000_000_001}} {
		if _, err := s.PublicProducts(context.Background(), "test", input); err == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
	if p.reads != 1 {
		t.Fatal("invalid pagination reached persistence")
	}
}

func TestCatalogServiceRequiresTrustedIdentity(t *testing.T) {
	s := management.New(nil, nil, nil)
	for _, identity := range []management.Identity{{}, {BusinessID: uuid.New()}, {ActorID: uuid.New()}} {
		if _, err := s.CreateProduct(context.Background(), identity, management.ProductInput{Name: "Coffee"}); err == nil {
			t.Fatal("accepted incomplete identity")
		}
		if _, err := s.CreateVariant(context.Background(), identity, management.VariantInput{ProductID: uuid.NewString(), SKU: "SKU", Price: "1"}); err == nil {
			t.Fatal("accepted incomplete identity")
		}
		if _, err := s.Products(context.Background(), identity, management.Page{}); err == nil {
			t.Fatal("accepted incomplete identity")
		}
	}
}
