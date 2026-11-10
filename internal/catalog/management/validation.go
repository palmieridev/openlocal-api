// Package management owns catalog use cases without transport or database dependencies.
package management

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
)

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func ParseProduct(req ProductInput) (ProductData, error) {
	categoryID, err := optionalUUID(req.CategoryID)
	if err != nil {
		return ProductData{}, invalid("category_id must be a uuid")
	}
	name := clean(req.Name)
	slug := slug(firstNonEmpty(req.Slug, name))
	if err := stringLength(name, "name", 2, 180); err != nil {
		return ProductData{}, err
	}
	if err := validateSlug(slug); err != nil {
		return ProductData{}, invalid(err.Error())
	}
	description := clean(req.Description)
	if err := stringLength(description, "description", 0, 5000); err != nil {
		return ProductData{}, err
	}
	brand := cleanOptional(req.Brand)
	if brand != nil {
		if err := stringLength(*brand, "brand", 1, 120); err != nil {
			return ProductData{}, err
		}
	}
	unit := firstNonEmpty(clean(req.Unit), "piece")
	if err := stringLength(unit, "unit", 1, 40); err != nil {
		return ProductData{}, err
	}
	productType, err := enum(req.ProductType, "product_type", "stocked_product", "stocked_product", "made_to_order_product", "unique_item")
	if err != nil {
		return ProductData{}, err
	}
	status, err := enum(req.Status, "status", "draft", "draft", "active", "archived")
	if err != nil {
		return ProductData{}, err
	}
	return ProductData{
		CategoryID:  categoryID,
		Name:        name,
		Slug:        slug,
		Description: description,
		Brand:       brand,
		Unit:        unit,
		ProductType: productType,
		IsHandmade:  req.IsHandmade,
		IsPublic:    req.IsPublic,
		Status:      status,
	}, nil
}

func ParseVariant(req VariantInput) (VariantData, error) {
	productID, err := parseUUID(req.ProductID, "product_id")
	if err != nil {
		return VariantData{}, err
	}
	price, err := parseDecimal(req.Price, "price")
	if err != nil {
		return VariantData{}, err
	}
	if err := decimalRange(price, "price", decimal.Zero, decimal.RequireFromString("9999999999.99"), 2); err != nil {
		return VariantData{}, err
	}
	cost, err := optionalDecimal(req.Cost)
	if err != nil {
		return VariantData{}, invalid("cost must be a decimal")
	}
	if cost != nil {
		if err := decimalRange(*cost, "cost", decimal.Zero, decimal.RequireFromString("9999999999.99"), 2); err != nil {
			return VariantData{}, err
		}
	}
	reorderPoint := decimal.Zero
	if req.ReorderPoint != "" {
		reorderPoint, err = parseDecimal(req.ReorderPoint, "reorder_point")
		if err != nil {
			return VariantData{}, err
		}
	}
	if err := decimalRange(reorderPoint, "reorder_point", decimal.Zero, decimal.RequireFromString("999999999.999"), 3); err != nil {
		return VariantData{}, err
	}
	if req.Attributes == nil {
		req.Attributes = map[string]any{}
	}
	attributes, err := json.Marshal(req.Attributes)
	if err != nil {
		return VariantData{}, invalid("attributes must be an object")
	}
	sku := strings.ToUpper(clean(req.SKU))
	internalCode := strings.ToUpper(clean(firstNonEmpty(req.InternalCode, sku)))
	if err := stringLength(sku, "sku", 2, 80); err != nil {
		return VariantData{}, err
	}
	if err := stringLength(internalCode, "internal_code", 2, 80); err != nil {
		return VariantData{}, err
	}
	name := clean(req.Name)
	if err := stringLength(name, "name", 0, 180); err != nil {
		return VariantData{}, err
	}
	description := cleanOptional(req.Description)
	if description != nil {
		if err := stringLength(*description, "description", 1, 2000); err != nil {
			return VariantData{}, err
		}
	}
	priceNote := cleanOptional(req.PriceNote)
	if priceNote != nil {
		if err := stringLength(*priceNote, "price_note", 1, 500); err != nil {
			return VariantData{}, err
		}
	}
	barcode := cleanOptional(req.Barcode)
	if barcode != nil {
		if err := stringLength(*barcode, "barcode", 1, 128); err != nil {
			return VariantData{}, err
		}
	}
	currency := strings.ToUpper(firstNonEmpty(clean(req.Currency), "MXN"))
	if !currencyPattern.MatchString(currency) {
		return VariantData{}, invalid("currency must be a three-letter code")
	}
	publicStockStatus, err := enum(req.PublicStockStatus, "public_stock_status", "unknown", "available", "low_stock", "out_of_stock", "made_to_order", "unknown")
	if err != nil {
		return VariantData{}, err
	}
	status, err := enum(req.Status, "status", "active", "active", "archived")
	if err != nil {
		return VariantData{}, err
	}
	if req.LeadTimeDays < 0 || req.LeadTimeDays > 3650 {
		return VariantData{}, invalid("lead_time_days must be between 0 and 3650")
	}
	return VariantData{
		ProductID:         productID,
		SKU:               sku,
		Barcode:           barcode,
		InternalCode:      internalCode,
		Name:              name,
		Description:       description,
		PriceNote:         priceNote,
		Attributes:        attributes,
		Price:             price,
		Cost:              cost,
		Currency:          currency,
		TrackInventory:    req.TrackInventory,
		PublicStockStatus: publicStockStatus,
		IsPublic:          req.IsPublic,
		ReorderPoint:      reorderPoint,
		LeadTimeDays:      req.LeadTimeDays,
		Status:            status,
	}, nil
}

func ParseImageURL(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		empty := ""
		return &empty, nil
	}
	if err := stringLength(value, "image_url", 1, 2048); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, invalid("image_url must be an absolute http(s) url")
	}
	return &value, nil
}
