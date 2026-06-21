package catalog

import (
	"encoding/json"
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
	BusinessID        string  `json:"business_id"`
	ProductID         string  `json:"product_id"`
	SKU               string  `json:"sku"`
	Barcode           *string `json:"barcode"`
	InternalCode      string  `json:"internal_code"`
	Name              string  `json:"name"`
	Attributes        JSONObj `json:"attributes"`
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
	Attributes        JSONObj    `json:"attributes,omitempty"`
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

func MapVariant(v db.ProductVariant, includePrivate bool) VariantResponse {
	out := VariantResponse{
		ID:                v.ID,
		ProductID:         v.ProductID,
		SKU:               v.Sku,
		Name:              v.Name,
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
	if name == "" {
		return db.CreateProductParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	if err := v.ValidateSlug(slug); err != nil {
		return db.CreateProductParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return db.CreateProductParams{
		BusinessID:  businessID,
		CategoryID:  categoryID,
		Name:        name,
		Slug:        slug,
		Description: v.Clean(req.Description),
		Brand:       api.NullString(v.CleanOptional(req.Brand)),
		Unit:        api.FirstNonEmpty(v.Clean(req.Unit), "piece"),
		ProductType: api.FirstNonEmpty(v.Slug(req.ProductType), "stocked_product"),
		IsHandmade:  req.IsHandmade,
		IsPublic:    req.IsPublic,
		Status:      api.FirstNonEmpty(v.Slug(req.Status), "draft"),
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
	price, err := decimal.NewFromString(req.Price)
	if err != nil || price.IsNegative() {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "price must be >= 0")
	}
	cost, err := api.NullDecimal(req.Cost)
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "cost must be a decimal")
	}
	reorderPoint := decimal.Zero
	if req.ReorderPoint != "" {
		reorderPoint, err = decimal.NewFromString(req.ReorderPoint)
		if err != nil || reorderPoint.IsNegative() {
			return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "reorder_point must be >= 0")
		}
	}
	attributes, err := json.Marshal(req.Attributes)
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "attributes must be an object")
	}
	sku := strings.ToUpper(v.Clean(req.SKU))
	internalCode := strings.ToUpper(v.Clean(api.FirstNonEmpty(req.InternalCode, sku)))
	if sku == "" || internalCode == "" {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "sku and internal_code are required")
	}
	return db.CreateVariantParams{
		ProductID:         productID,
		BusinessID:        businessID,
		Sku:               sku,
		Barcode:           api.NullString(v.CleanOptional(req.Barcode)),
		InternalCode:      internalCode,
		Name:              v.Clean(req.Name),
		Attributes:        attributes,
		Price:             price,
		Cost:              cost,
		Currency:          strings.ToUpper(api.FirstNonEmpty(v.Clean(req.Currency), "MXN")),
		TrackInventory:    req.TrackInventory,
		PublicStockStatus: api.FirstNonEmpty(v.Slug(req.PublicStockStatus), "unknown"),
		ReorderPoint:      reorderPoint,
		LeadTimeDays:      req.LeadTimeDays,
		Status:            api.FirstNonEmpty(v.Slug(req.Status), "active"),
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
			"price":               row.Price.StringFixed(2),
			"currency":            row.Currency,
			"public_stock_status": row.PublicStockStatus,
		})
	}
	return out
}
