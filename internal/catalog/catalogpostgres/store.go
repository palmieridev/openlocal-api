// Package catalogpostgres translates catalog ports to tenant-scoped sqlc queries.
package catalogpostgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/catalog/management"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool, q *db.Queries) *Store { return &Store{pool: pool, q: q} }

var _ management.Products = (*Store)(nil)
var _ management.Variants = (*Store)(nil)
var _ management.PublicProducts = (*Store)(nil)
var _ management.VariantTransaction = (*transaction)(nil)

func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return management.ErrNotFound
	}
	return err
}

func (s *Store) RequireCategory(ctx context.Context, businessID, id uuid.UUID) error {
	_, err := s.q.GetCatalogCategoryForBusiness(ctx, db.GetCatalogCategoryForBusinessParams{ID: id, BusinessID: businessID})
	return translate(err)
}

func (s *Store) CreateProduct(ctx context.Context, businessID uuid.UUID, data management.ProductData) (management.Product, error) {
	params := CreateProductParams(businessID, data)
	row, err := s.q.CreateProduct(ctx, params)
	return ProductResult(row), translate(err)
}

func CreateProductParams(businessID uuid.UUID, data management.ProductData) db.CreateProductParams {
	return db.CreateProductParams{
		BusinessID:  businessID,
		CategoryID:  nullUUID(data.CategoryID),
		Name:        data.Name,
		Slug:        data.Slug,
		Description: data.Description,
		Brand:       nullString(data.Brand),
		Unit:        data.Unit,
		ProductType: data.ProductType,
		IsHandmade:  data.IsHandmade,
		IsPublic:    data.IsPublic,
		Status:      data.Status,
	}
}

func (s *Store) UpdateProduct(ctx context.Context, businessID, id uuid.UUID, data management.ProductData) (management.Product, error) {
	params := UpdateProductParams(businessID, id, data)
	row, err := s.q.UpdateProduct(ctx, params)
	return ProductResult(row), translate(err)
}

func UpdateProductParams(businessID, id uuid.UUID, data management.ProductData) db.UpdateProductParams {
	return db.UpdateProductParams{
		BusinessID:  businessID,
		ID:          id,
		CategoryID:  nullUUID(data.CategoryID),
		Name:        data.Name,
		Description: data.Description,
		Brand:       nullString(data.Brand),
		Unit:        data.Unit,
		ProductType: data.ProductType,
		IsHandmade:  data.IsHandmade,
		IsPublic:    data.IsPublic,
		Status:      data.Status,
	}
}

func ProductResult(row db.Product) management.Product {
	return management.Product{
		ID: row.ID, BusinessID: row.BusinessID, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		ProductData: management.ProductData{
			CategoryID:  uuidPtr(row.CategoryID),
			Name:        row.Name,
			Slug:        row.Slug,
			Description: row.Description,
			Brand:       stringPtr(row.Brand),
			Unit:        row.Unit,
			ProductType: row.ProductType,
			IsHandmade:  row.IsHandmade,
			IsPublic:    row.IsPublic,
			Status:      row.Status,
		}}
}

func (s *transaction) CreateVariant(ctx context.Context, businessID uuid.UUID, data management.VariantData) (management.Variant, error) {
	params := CreateVariantParams(businessID, data)
	row, err := s.q.CreateVariant(ctx, params)
	return VariantResult(row, ""), translate(err)
}

func CreateVariantParams(businessID uuid.UUID, data management.VariantData) db.CreateVariantParams {
	return db.CreateVariantParams{
		BusinessID:        businessID,
		ProductID:         data.ProductID,
		Sku:               data.SKU,
		Barcode:           nullString(data.Barcode),
		InternalCode:      data.InternalCode,
		Name:              data.Name,
		Description:       nullString(data.Description),
		PriceNote:         nullString(data.PriceNote),
		Attributes:        data.Attributes,
		Price:             data.Price,
		Cost:              nullDecimal(data.Cost),
		Currency:          data.Currency,
		TrackInventory:    data.TrackInventory,
		PublicStockStatus: data.PublicStockStatus,
		IsPublic:          boolValue(data.IsPublic),
		ReorderPoint:      data.ReorderPoint,
		LeadTimeDays:      data.LeadTimeDays,
		Status:            data.Status,
	}
}

func (s *transaction) UpdateVariant(ctx context.Context, businessID, id uuid.UUID, data management.VariantData) (management.Variant, error) {
	params := UpdateVariantParams(businessID, id, data)
	row, err := s.q.UpdateVariant(ctx, params)
	return VariantResult(row, ""), translate(err)
}

func UpdateVariantParams(businessID, id uuid.UUID, data management.VariantData) db.UpdateVariantParams {
	return db.UpdateVariantParams{
		BusinessID:        businessID,
		ID:                id,
		Sku:               data.SKU,
		Barcode:           nullString(data.Barcode),
		InternalCode:      data.InternalCode,
		Name:              data.Name,
		Description:       nullString(data.Description),
		PriceNote:         nullString(data.PriceNote),
		Attributes:        data.Attributes,
		Price:             data.Price,
		Cost:              nullDecimal(data.Cost),
		Currency:          data.Currency,
		TrackInventory:    data.TrackInventory,
		PublicStockStatus: data.PublicStockStatus,
		IsPublic:          nullBool(data.IsPublic),
		ReorderPoint:      data.ReorderPoint,
		LeadTimeDays:      data.LeadTimeDays,
		Status:            data.Status,
	}
}

func VariantResult(row db.ProductVariant, image string) management.Variant {
	return management.Variant{
		ID: row.ID, BusinessID: row.BusinessID, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		ImageURL: imagePtr(image),
		VariantData: management.VariantData{
			ProductID:         row.ProductID,
			SKU:               row.Sku,
			Barcode:           stringPtr(row.Barcode),
			InternalCode:      row.InternalCode,
			Name:              row.Name,
			Description:       stringPtr(row.Description),
			PriceNote:         stringPtr(row.PriceNote),
			Attributes:        row.Attributes,
			Price:             row.Price,
			Cost:              decimalPtr(row.Cost),
			Currency:          row.Currency,
			TrackInventory:    row.TrackInventory,
			PublicStockStatus: row.PublicStockStatus,
			IsPublic:          &row.IsPublic,
			ReorderPoint:      row.ReorderPoint,
			LeadTimeDays:      row.LeadTimeDays,
			Status:            row.Status,
		}}
}

func (s *Store) Product(ctx context.Context, businessID, id uuid.UUID) (management.Product, error) {
	row, err := s.q.GetProductForBusiness(ctx, db.GetProductForBusinessParams{ID: id, BusinessID: businessID})
	return ProductResult(row), translate(err)
}

func (s *Store) Products(ctx context.Context, businessID uuid.UUID, page management.Page) ([]management.Product, error) {
	rows, err := s.q.ListProducts(ctx, db.ListProductsParams{BusinessID: businessID, Limit: page.Limit, Offset: page.Offset})
	if err != nil {
		return nil, translate(err)
	}
	out := make([]management.Product, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProductResult(row))
	}
	return out, nil
}

func (s *Store) ArchiveProduct(ctx context.Context, businessID, id uuid.UUID) error {
	return translate(s.q.ArchiveProduct(ctx, db.ArchiveProductParams{ID: id, BusinessID: businessID}))
}

func (s *Store) Variant(ctx context.Context, businessID, id uuid.UUID) (management.Variant, error) {
	row, err := s.q.GetVariantForBusiness(ctx, db.GetVariantForBusinessParams{ID: id, BusinessID: businessID})
	return VariantResult(row.ProductVariant, row.ImageUrl), translate(err)
}

func (s *Store) Variants(ctx context.Context, businessID, productID uuid.UUID, page management.Page) ([]management.Variant, error) {
	rows, err := s.q.ListVariantsByProduct(ctx, db.ListVariantsByProductParams{ProductID: productID, BusinessID: businessID, LimitCount: page.Limit, OffsetCount: page.Offset})
	if err != nil {
		return nil, translate(err)
	}
	out := make([]management.Variant, 0, len(rows))
	for _, row := range rows {
		out = append(out, VariantResult(row.ProductVariant, row.ImageUrl))
	}
	return out, nil
}

func (s *Store) BySKU(ctx context.Context, businessID uuid.UUID, sku string) (management.Variant, error) {
	row, err := s.q.GetVariantBySKU(ctx, db.GetVariantBySKUParams{BusinessID: businessID, Sku: sku})
	return VariantResult(row.ProductVariant, row.ImageUrl), translate(err)
}

func (s *Store) ByBarcode(ctx context.Context, businessID uuid.UUID, barcode string) (management.Variant, error) {
	row, err := s.q.GetVariantByBarcode(ctx, db.GetVariantByBarcodeParams{BusinessID: businessID, Barcode: sql.NullString{String: barcode, Valid: true}})
	return VariantResult(row.ProductVariant, row.ImageUrl), translate(err)
}

func (s *Store) ArchiveVariant(ctx context.Context, businessID, id uuid.UUID) error {
	return translate(s.q.ArchiveVariant(ctx, db.ArchiveVariantParams{ID: id, BusinessID: businessID}))
}

func (s *Store) PublicProducts(ctx context.Context, slug string, page management.Page) ([]management.PublicProduct, error) {
	rows, err := s.q.ListPublicProductsByBusinessSlug(ctx, db.ListPublicProductsByBusinessSlugParams{Slug: slug, Limit: page.Limit, Offset: page.Offset})
	if err != nil {
		return nil, translate(err)
	}
	out := make([]management.PublicProduct, 0, len(rows))
	for _, row := range rows {
		out = append(out, PublicProductResult(row))
	}
	return out, nil
}

func PublicProductResult(row db.ListPublicProductsByBusinessSlugRow) management.PublicProduct {
	return management.PublicProduct{
		ID: row.ID, VariantID: row.VariantID, Name: row.Name, Slug: row.Slug,
		Description: row.Description, Brand: stringPtr(row.Brand),
		Unit: row.Unit, ProductType: row.ProductType, SKU: row.Sku,
		VariantName: row.VariantName, VariantDescription: stringPtr(row.VariantDescription),
		PriceNote: stringPtr(row.PriceNote), Price: row.Price, Currency: row.Currency,
		PublicStockStatus: row.PublicStockStatus, ImageURL: imagePtr(row.ImageUrl),
	}
}

type transaction struct {
	tx pgx.Tx
	q  *db.Queries
}

func (s *Store) Begin(ctx context.Context) (management.VariantTransaction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &transaction{tx: tx, q: s.q.WithTx(tx)}, nil
}

func (s *transaction) Commit(ctx context.Context) error { return s.tx.Commit(ctx) }

func (s *transaction) Rollback(ctx context.Context) error { return s.tx.Rollback(ctx) }

func (s *transaction) RequireProduct(ctx context.Context, businessID, id uuid.UUID) error {
	_, err := s.q.GetProductForBusiness(ctx, db.GetProductForBusinessParams{ID: id, BusinessID: businessID})
	return translate(err)
}

func (s *transaction) LockVariant(ctx context.Context, businessID, id uuid.UUID) (management.Variant, error) {
	row, err := s.q.LockCatalogVariantForBusiness(ctx, db.LockCatalogVariantForBusinessParams{ID: id, BusinessID: businessID})
	return VariantResult(row, ""), translate(err)
}

func (s *transaction) ApplyImage(ctx context.Context, id uuid.UUID, target *string) (*string, error) {
	if target == nil {
		current, err := s.q.GetVariantImage(ctx, id)
		return imagePtr(current), err
	}
	if err := s.q.DeleteVariantImages(ctx, id); err != nil {
		return nil, err
	}
	if *target == "" {
		return nil, nil
	}
	if err := s.q.CreateVariantImage(ctx, db.CreateVariantImageParams{VariantID: id, Url: *target}); err != nil {
		return nil, err
	}
	return target, nil
}

func nullUUID(v *uuid.UUID) uuid.NullUUID {
	if v == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *v, Valid: true}
}

func uuidPtr(v uuid.NullUUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	return &v.UUID
}

func nullString(v *string) sql.NullString {
	if v == nil || *v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullDecimal(v *decimal.Decimal) decimal.NullDecimal {
	if v == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: *v, Valid: true}
}

func decimalPtr(v decimal.NullDecimal) *decimal.Decimal {
	if !v.Valid {
		return nil
	}
	return &v.Decimal
}

func nullBool(v *bool) pgtype.Bool {
	if v == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *v, Valid: true}
}

func boolValue(v *bool) bool { return v != nil && *v }

func imagePtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}
