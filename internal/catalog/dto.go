package catalog

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/shopspring/decimal"
)

type JSONObj map[string]any

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

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
	ReorderPoint      string     `json:"reorder_point,omitempty"`
	LeadTimeDays      int32      `json:"lead_time_days,omitempty"`
	Status            string     `json:"status,omitempty"`
	CreatedAt         *time.Time `json:"created_at,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

func MapProduct(p db.Product, includePrivate bool) ProductResponse {
	out := ProductResponse{
		ID:          p.ID,
		CategoryID:  api.UUIDPtr(p.CategoryID),
		Name:        p.Name,
		Slug:        p.Slug,
		Description: p.Description,
		Brand:       api.StringPtr(p.Brand),
		Unit:        p.Unit,
		ProductType: p.ProductType,
		IsHandmade:  p.IsHandmade,
	}
	if includePrivate {
		out.BusinessID = &p.BusinessID
		out.IsPublic = p.IsPublic
		out.Status = p.Status
		out.CreatedAt = api.TSP(p.CreatedAt)
		out.UpdatedAt = api.TSP(p.UpdatedAt)
	}
	return out
}

func MapVariant(v db.ProductVariant, imageURL string, includePrivate bool) VariantResponse {
	out := VariantResponse{
		ID:                v.ID,
		ProductID:         v.ProductID,
		SKU:               v.Sku,
		Name:              v.Name,
		Description:       api.StringPtr(v.Description),
		PriceNote:         api.StringPtr(v.PriceNote),
		ImageURL:          stringPtr(imageURL),
		Price:             v.Price.StringFixed(2),
		Currency:          v.Currency,
		PublicStockStatus: v.PublicStockStatus,
	}
	if includePrivate {
		out.BusinessID = &v.BusinessID
		out.Barcode = api.StringPtr(v.Barcode)
		out.InternalCode = v.InternalCode
		out.Cost = api.DecimalPtr(v.Cost)
		out.TrackInventory = v.TrackInventory
		out.ReorderPoint = v.ReorderPoint.String()
		out.LeadTimeDays = v.LeadTimeDays
		out.Status = v.Status
		out.CreatedAt = api.TSP(v.CreatedAt)
		out.UpdatedAt = api.TSP(v.UpdatedAt)
	}
	return out
}

func ProductParams(req ProductRequest) (db.CreateProductParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateProductParams{}, uuid.Nil, err
	}
	categoryID, err := api.NullUUID(req.CategoryID)
	if err != nil {
		return db.CreateProductParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "category_id must be a uuid")
	}
	name := v.Clean(req.Name)
	slug := v.Slug(api.FirstNonEmpty(req.Slug, name))
	if err := v.StringLength(name, "name", 2, 180); err != nil {
		return db.CreateProductParams{}, uuid.Nil, err
	}
	if err := v.ValidateSlug(slug); err != nil {
		return db.CreateProductParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	description := v.Clean(req.Description)
	if err := v.StringLength(description, "description", 0, 5000); err != nil {
		return db.CreateProductParams{}, uuid.Nil, err
	}
	brand := v.CleanOptional(req.Brand)
	if brand != nil {
		if err := v.StringLength(*brand, "brand", 1, 120); err != nil {
			return db.CreateProductParams{}, uuid.Nil, err
		}
	}
	unit := api.FirstNonEmpty(v.Clean(req.Unit), "piece")
	if err := v.StringLength(unit, "unit", 1, 40); err != nil {
		return db.CreateProductParams{}, uuid.Nil, err
	}
	productType, err := v.Enum(req.ProductType, "product_type", "stocked_product", "stocked_product", "made_to_order_product", "unique_item")
	if err != nil {
		return db.CreateProductParams{}, uuid.Nil, err
	}
	status, err := v.Enum(req.Status, "status", "draft", "draft", "active", "archived")
	if err != nil {
		return db.CreateProductParams{}, uuid.Nil, err
	}
	return db.CreateProductParams{
		BusinessID:  businessID,
		CategoryID:  categoryID,
		Name:        name,
		Slug:        slug,
		Description: description,
		Brand:       api.NullString(brand),
		Unit:        unit,
		ProductType: productType,
		IsHandmade:  req.IsHandmade,
		IsPublic:    req.IsPublic,
		Status:      status,
	}, businessID, nil
}

func UpdateProductParams(id uuid.UUID, req ProductRequest) (db.UpdateProductParams, uuid.UUID, error) {
	params, businessID, err := ProductParams(req)
	if err != nil {
		return db.UpdateProductParams{}, uuid.Nil, err
	}
	return db.UpdateProductParams{
		ID:          id,
		BusinessID:  businessID,
		CategoryID:  params.CategoryID,
		Name:        params.Name,
		Description: params.Description,
		Brand:       params.Brand,
		Unit:        params.Unit,
		ProductType: params.ProductType,
		IsHandmade:  params.IsHandmade,
		IsPublic:    params.IsPublic,
		Status:      params.Status,
	}, businessID, nil
}

func VariantParams(req VariantRequest) (db.CreateVariantParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	productID, err := v.ParseUUID(req.ProductID, "product_id")
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	price, err := v.ParseDecimal(req.Price, "price")
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	if err := v.DecimalRange(price, "price", decimal.Zero, decimal.RequireFromString("9999999999.99"), 2); err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	cost, err := api.NullDecimal(req.Cost)
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "cost must be a decimal")
	}
	if cost.Valid {
		if err := v.DecimalRange(cost.Decimal, "cost", decimal.Zero, decimal.RequireFromString("9999999999.99"), 2); err != nil {
			return db.CreateVariantParams{}, uuid.Nil, err
		}
	}
	reorderPoint := decimal.Zero
	if req.ReorderPoint != "" {
		reorderPoint, err = v.ParseDecimal(req.ReorderPoint, "reorder_point")
		if err != nil {
			return db.CreateVariantParams{}, uuid.Nil, err
		}
	}
	if err := v.DecimalRange(reorderPoint, "reorder_point", decimal.Zero, decimal.RequireFromString("999999999.999"), 3); err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	if req.Attributes == nil {
		req.Attributes = JSONObj{}
	}
	attributes, err := json.Marshal(req.Attributes)
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "attributes must be an object")
	}
	sku := strings.ToUpper(v.Clean(req.SKU))
	internalCode := strings.ToUpper(v.Clean(api.FirstNonEmpty(req.InternalCode, sku)))
	if err := v.StringLength(sku, "sku", 2, 80); err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	if err := v.StringLength(internalCode, "internal_code", 2, 80); err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	name := v.Clean(req.Name)
	if err := v.StringLength(name, "name", 0, 180); err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	description := v.CleanOptional(req.Description)
	if description != nil {
		if err := v.StringLength(*description, "description", 1, 2000); err != nil {
			return db.CreateVariantParams{}, uuid.Nil, err
		}
	}
	priceNote := v.CleanOptional(req.PriceNote)
	if priceNote != nil {
		if err := v.StringLength(*priceNote, "price_note", 1, 500); err != nil {
			return db.CreateVariantParams{}, uuid.Nil, err
		}
	}
	barcode := v.CleanOptional(req.Barcode)
	if barcode != nil {
		if err := v.StringLength(*barcode, "barcode", 1, 128); err != nil {
			return db.CreateVariantParams{}, uuid.Nil, err
		}
	}
	currency := strings.ToUpper(api.FirstNonEmpty(v.Clean(req.Currency), "MXN"))
	if !currencyPattern.MatchString(currency) {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "currency must be a three-letter code")
	}
	publicStockStatus, err := v.Enum(req.PublicStockStatus, "public_stock_status", "unknown", "available", "low_stock", "out_of_stock", "made_to_order", "unknown")
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	status, err := v.Enum(req.Status, "status", "active", "active", "archived")
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	if req.LeadTimeDays < 0 || req.LeadTimeDays > 3650 {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "lead_time_days must be between 0 and 3650")
	}
	return db.CreateVariantParams{
		ProductID:         productID,
		BusinessID:        businessID,
		Sku:               sku,
		Barcode:           api.NullString(barcode),
		InternalCode:      internalCode,
		Name:              name,
		Description:       api.NullString(description),
		PriceNote:         api.NullString(priceNote),
		Attributes:        attributes,
		Price:             price,
		Cost:              cost,
		Currency:          currency,
		TrackInventory:    req.TrackInventory,
		PublicStockStatus: publicStockStatus,
		ReorderPoint:      reorderPoint,
		LeadTimeDays:      req.LeadTimeDays,
		Status:            status,
	}, businessID, nil
}

func UpdateVariantParams(id uuid.UUID, req VariantRequest) (db.UpdateVariantParams, uuid.UUID, error) {
	params, businessID, err := VariantParams(req)
	if err != nil {
		return db.UpdateVariantParams{}, uuid.Nil, err
	}
	return db.UpdateVariantParams{
		ID:                id,
		BusinessID:        businessID,
		Sku:               params.Sku,
		Barcode:           params.Barcode,
		InternalCode:      params.InternalCode,
		Name:              params.Name,
		Description:       params.Description,
		PriceNote:         params.PriceNote,
		Attributes:        params.Attributes,
		Price:             params.Price,
		Cost:              params.Cost,
		Currency:          params.Currency,
		TrackInventory:    params.TrackInventory,
		PublicStockStatus: params.PublicStockStatus,
		ReorderPoint:      params.ReorderPoint,
		LeadTimeDays:      params.LeadTimeDays,
		Status:            params.Status,
	}, businessID, nil
}

func PublicProductRows(rows []db.ListPublicProductsByBusinessSlugRow) []fiber.Map {
	out := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		out = append(out, fiber.Map{
			"id":                  row.ID,
			"name":                row.Name,
			"slug":                row.Slug,
			"description":         row.Description,
			"brand":               api.StringPtr(row.Brand),
			"unit":                row.Unit,
			"product_type":        row.ProductType,
			"variant_id":          row.VariantID,
			"sku":                 row.Sku,
			"variant_name":        row.VariantName,
			"variant_description": api.StringPtr(row.VariantDescription),
			"price_note":          api.StringPtr(row.PriceNote),
			"price":               row.Price.StringFixed(2),
			"currency":            row.Currency,
			"public_stock_status": row.PublicStockStatus,
			"image_url":           stringPtr(row.ImageUrl),
		})
	}
	return out
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// VariantImageURL validates the image_url field of a variant request.
//
// Returns (nil, nil) when the field was omitted or null — meaning "leave the
// current image alone". A present-but-empty value yields ("", nil): an explicit
// request to remove the image. Anything else must be an absolute http(s) URL.
func VariantImageURL(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		empty := ""
		return &empty, nil
	}
	if err := v.StringLength(value, "image_url", 1, 2048); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fiber.NewError(fiber.StatusBadRequest, "image_url must be an absolute http(s) url")
	}
	return &value, nil
}
