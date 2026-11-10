package catalog

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/catalog/catalogpostgres"
	"github.com/palmieridev/openlocal-api/internal/catalog/management"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type JSONObj map[string]any
type ProductRequest struct {
	BusinessID  string  `json:"business_id"`
	CategoryID  *string `json:"category_id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description"`
	Brand       *string `json:"brand"`
	Unit        string  `json:"unit"`
	ProductType string  `json:"product_type"`
	IsHandmade  bool    `json:"is_handmade"`
	IsPublic    bool    `json:"is_public"`
	Status      string  `json:"status"`
}

type ProductResponse struct {
	ID          uuid.UUID  `json:"id"`
	BusinessID  *uuid.UUID `json:"business_id,omitempty"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description"`
	Brand       *string    `json:"brand,omitempty"`
	Unit        string     `json:"unit"`
	ProductType string     `json:"product_type"`
	IsHandmade  bool       `json:"is_handmade,omitempty"`
	IsPublic    bool       `json:"is_public,omitempty"`
	Status      string     `json:"status,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

type VariantRequest struct {
	BusinessID   string  `json:"business_id"`
	ProductID    string  `json:"product_id"`
	SKU          string  `json:"sku"`
	Barcode      *string `json:"barcode"`
	InternalCode string  `json:"internal_code"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	PriceNote    *string `json:"price_note"`
	Attributes   JSONObj `json:"attributes"`
	// ImageURL is the variant's storefront image. Omitted or null leaves the
	// current image untouched — an empty string removes it. Callers that never
	// send the field therefore can't wipe an image by accident.
	ImageURL          *string `json:"image_url"`
	Price             string  `json:"price"`
	Cost              *string `json:"cost"`
	Currency          string  `json:"currency"`
	TrackInventory    bool    `json:"track_inventory"`
	PublicStockStatus string  `json:"public_stock_status"`
	IsPublic          *bool   `json:"is_public"`
	ReorderPoint      string  `json:"reorder_point"`
	LeadTimeDays      int32   `json:"lead_time_days"`
	Status            string  `json:"status"`
}

type VariantResponse struct {
	ID                uuid.UUID  `json:"id"`
	ProductID         uuid.UUID  `json:"product_id"`
	BusinessID        *uuid.UUID `json:"business_id,omitempty"`
	SKU               string     `json:"sku"`
	Barcode           *string    `json:"barcode,omitempty"`
	InternalCode      string     `json:"internal_code,omitempty"`
	Name              string     `json:"name"`
	Description       *string    `json:"description,omitempty"`
	PriceNote         *string    `json:"price_note,omitempty"`
	Attributes        JSONObj    `json:"attributes,omitempty"`
	ImageURL          *string    `json:"image_url,omitempty"`
	Price             string     `json:"price"`
	Cost              *string    `json:"cost,omitempty"`
	Currency          string     `json:"currency"`
	TrackInventory    bool       `json:"track_inventory,omitempty"`
	PublicStockStatus string     `json:"public_stock_status"`
	IsPublic          *bool      `json:"is_public,omitempty"`
	ReorderPoint      string     `json:"reorder_point,omitempty"`
	LeadTimeDays      int32      `json:"lead_time_days,omitempty"`
	Status            string     `json:"status,omitempty"`
	CreatedAt         *time.Time `json:"created_at,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

func httpError(err error) error {
	var validation *management.ValidationError
	if errors.As(err, &validation) {
		return fiber.NewError(fiber.StatusBadRequest, validation.Error())
	}
	if errors.Is(err, management.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "not found")
	}
	return err
}

func productInput(req ProductRequest) management.ProductInput {
	return management.ProductInput{
		CategoryID: req.CategoryID, Name: req.Name, Slug: req.Slug,
		Description: req.Description, Brand: req.Brand, Unit: req.Unit,
		ProductType: req.ProductType, IsHandmade: req.IsHandmade,
		IsPublic: req.IsPublic, Status: req.Status,
	}
}

func variantInput(req VariantRequest) management.VariantInput {
	return management.VariantInput{
		ProductID: req.ProductID, SKU: req.SKU, Barcode: req.Barcode,
		InternalCode: req.InternalCode, Name: req.Name, Description: req.Description,
		PriceNote: req.PriceNote, Attributes: req.Attributes, ImageURL: req.ImageURL,
		Price: req.Price, Cost: req.Cost, Currency: req.Currency,
		TrackInventory: req.TrackInventory, PublicStockStatus: req.PublicStockStatus,
		IsPublic: req.IsPublic, ReorderPoint: req.ReorderPoint,
		LeadTimeDays: req.LeadTimeDays, Status: req.Status,
	}
}

// Compatibility wrappers keep existing consumers on the same DTO/sqlc contracts.
func ProductParams(req ProductRequest) (db.CreateProductParams, uuid.UUID, error) {
	id, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateProductParams{}, uuid.Nil, err
	}
	data, err := management.ParseProduct(productInput(req))
	if err != nil {
		return db.CreateProductParams{}, uuid.Nil, httpError(err)
	}
	return catalogpostgres.CreateProductParams(id, data), id, nil
}

func UpdateProductParams(id uuid.UUID, req ProductRequest) (db.UpdateProductParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.UpdateProductParams{}, uuid.Nil, err
	}
	data, err := management.ParseProduct(productInput(req))
	if err != nil {
		return db.UpdateProductParams{}, uuid.Nil, httpError(err)
	}
	return catalogpostgres.UpdateProductParams(businessID, id, data), businessID, nil
}

func VariantParams(req VariantRequest) (db.CreateVariantParams, uuid.UUID, error) {
	id, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	data, err := management.ParseVariant(variantInput(req))
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, httpError(err)
	}
	if data.IsPublic == nil {
		value := true
		data.IsPublic = &value
	}
	return catalogpostgres.CreateVariantParams(id, data), id, nil
}

func UpdateVariantParams(id uuid.UUID, req VariantRequest) (db.UpdateVariantParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.UpdateVariantParams{}, uuid.Nil, err
	}
	data, err := management.ParseVariant(variantInput(req))
	if err != nil {
		return db.UpdateVariantParams{}, uuid.Nil, httpError(err)
	}
	return catalogpostgres.UpdateVariantParams(businessID, id, data), businessID, nil
}

func VariantImageURL(raw *string) (*string, error) {
	value, err := management.ParseImageURL(raw)
	return value, httpError(err)
}

func MapProduct(p db.Product, includePrivate bool) ProductResponse {
	return productResponse(catalogpostgres.ProductResult(p), includePrivate)
}

func MapVariant(v db.ProductVariant, imageURL string, includePrivate bool) VariantResponse {
	return variantResponse(catalogpostgres.VariantResult(v, imageURL), includePrivate)
}

func PublicProductRows(rows []db.ListPublicProductsByBusinessSlugRow) []fiber.Map {
	out := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		out = append(out, publicProductResponse(catalogpostgres.PublicProductResult(row)))
	}
	return out
}

func productResponse(p management.Product, includePrivate bool) ProductResponse {
	out := ProductResponse{
		ID: p.ID, CategoryID: p.CategoryID, Name: p.Name, Slug: p.Slug,
		Description: p.Description, Brand: p.Brand, Unit: p.Unit,
		ProductType: p.ProductType, IsHandmade: p.IsHandmade,
	}
	if includePrivate {
		out.BusinessID = &p.BusinessID
		out.IsPublic = p.IsPublic
		out.Status = p.Status
		out.CreatedAt = timePtr(p.CreatedAt)
		out.UpdatedAt = timePtr(p.UpdatedAt)
	}
	return out
}

func variantResponse(v management.Variant, includePrivate bool) VariantResponse {
	out := VariantResponse{
		ID: v.ID, ProductID: v.ProductID, SKU: v.SKU, Name: v.Name,
		Description: v.Description, PriceNote: v.PriceNote, ImageURL: v.ImageURL,
		Price: v.Price.StringFixed(2), Currency: v.Currency,
		PublicStockStatus: v.PublicStockStatus,
	}
	if includePrivate {
		out.BusinessID = &v.BusinessID
		out.Barcode = v.Barcode
		out.InternalCode = v.InternalCode
		if v.Cost != nil {
			value := v.Cost.String()
			out.Cost = &value
		}
		out.TrackInventory = v.TrackInventory
		out.IsPublic = v.IsPublic
		out.ReorderPoint = v.ReorderPoint.String()
		out.LeadTimeDays = v.LeadTimeDays
		out.Status = v.Status
		out.CreatedAt = timePtr(v.CreatedAt)
		out.UpdatedAt = timePtr(v.UpdatedAt)
	}
	return out
}

func publicProductResponse(row management.PublicProduct) fiber.Map {
	return fiber.Map{
		"id":                  row.ID,
		"name":                row.Name,
		"slug":                row.Slug,
		"description":         row.Description,
		"brand":               row.Brand,
		"unit":                row.Unit,
		"product_type":        row.ProductType,
		"variant_id":          row.VariantID,
		"sku":                 row.SKU,
		"variant_name":        row.VariantName,
		"variant_description": row.VariantDescription,
		"price_note":          row.PriceNote,
		"price":               row.Price.StringFixed(2),
		"currency":            row.Currency,
		"public_stock_status": row.PublicStockStatus,
		"image_url":           row.ImageURL,
	}
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
