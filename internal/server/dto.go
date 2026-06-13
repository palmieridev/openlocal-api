package server

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

type errorResponse struct {
	Error string `json:"error"`
}

type meResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     *string   `json:"email,omitempty"`
	FirstName *string   `json:"first_name,omitempty"`
	LastName  *string   `json:"last_name,omitempty"`
	ImageURL  *string   `json:"image_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type businessRequest struct {
	Name              string  `json:"name"`
	Slug              string  `json:"slug"`
	Description       string  `json:"description"`
	BusinessType      string  `json:"business_type"`
	Phone             *string `json:"phone"`
	Whatsapp          *string `json:"whatsapp"`
	Email             *string `json:"email"`
	Website           *string `json:"website"`
	LogoURL           *string `json:"logo_url"`
	CoverImageURL     *string `json:"cover_image_url"`
	Status            string  `json:"status"`
	Address           *string `json:"address"`
	Neighborhood      *string `json:"neighborhood"`
	City              string  `json:"city"`
	State             string  `json:"state"`
	Country           string  `json:"country"`
	PostalCode        *string `json:"postal_code"`
	Latitude          *string `json:"latitude"`
	Longitude         *string `json:"longitude"`
	PickupAvailable   bool    `json:"pickup_available"`
	DeliveryAvailable bool    `json:"delivery_available"`
}

type businessResponse struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	Description       string    `json:"description"`
	BusinessType      string    `json:"business_type"`
	Phone             *string   `json:"phone,omitempty"`
	Whatsapp          *string   `json:"whatsapp,omitempty"`
	Email             *string   `json:"email,omitempty"`
	Website           *string   `json:"website,omitempty"`
	LogoURL           *string   `json:"logo_url,omitempty"`
	CoverImageURL     *string   `json:"cover_image_url,omitempty"`
	Status            string    `json:"status,omitempty"`
	Address           *string   `json:"address,omitempty"`
	Neighborhood      *string   `json:"neighborhood,omitempty"`
	City              string    `json:"city"`
	State             string    `json:"state"`
	Country           string    `json:"country"`
	PostalCode        *string   `json:"postal_code,omitempty"`
	Latitude          *string   `json:"latitude,omitempty"`
	Longitude         *string   `json:"longitude,omitempty"`
	PickupAvailable   bool      `json:"pickup_available"`
	DeliveryAvailable bool      `json:"delivery_available"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

type productRequest struct {
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

type productResponse struct {
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

type variantRequest struct {
	BusinessID        string     `json:"business_id"`
	ProductID         string     `json:"product_id"`
	SKU               string     `json:"sku"`
	Barcode           *string    `json:"barcode"`
	InternalCode      string     `json:"internal_code"`
	Name              string     `json:"name"`
	Attributes        jsonObject `json:"attributes"`
	Price             string     `json:"price"`
	Cost              *string    `json:"cost"`
	Currency          string     `json:"currency"`
	TrackInventory    bool       `json:"track_inventory"`
	PublicStockStatus string     `json:"public_stock_status"`
	ReorderPoint      string     `json:"reorder_point"`
	LeadTimeDays      int32      `json:"lead_time_days"`
	Status            string     `json:"status"`
}

type jsonObject map[string]any

type variantResponse struct {
	ID                uuid.UUID  `json:"id"`
	ProductID         uuid.UUID  `json:"product_id"`
	BusinessID        *uuid.UUID `json:"business_id,omitempty"`
	SKU               string     `json:"sku"`
	Barcode           *string    `json:"barcode,omitempty"`
	InternalCode      string     `json:"internal_code,omitempty"`
	Name              string     `json:"name"`
	Attributes        jsonObject `json:"attributes,omitempty"`
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

type stockMovementRequest struct {
	BusinessID    string  `json:"business_id"`
	VariantID     string  `json:"variant_id"`
	LocationID    *string `json:"location_id"`
	MovementType  string  `json:"movement_type"`
	Quantity      string  `json:"quantity"`
	UnitCost      *string `json:"unit_cost"`
	ReferenceType *string `json:"reference_type"`
	ReferenceID   *string `json:"reference_id"`
	Notes         string  `json:"notes"`
}

func nullString(value *string) sql.NullString {
	if value == nil || *value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullDecimal(value *string) (decimal.NullDecimal, error) {
	if value == nil || *value == "" {
		return decimal.NullDecimal{}, nil
	}
	d, err := decimal.NewFromString(*value)
	if err != nil {
		return decimal.NullDecimal{}, err
	}
	return decimal.NullDecimal{Decimal: d, Valid: true}, nil
}

func decimalPtr(value decimal.NullDecimal) *string {
	if !value.Valid {
		return nil
	}
	s := value.Decimal.String()
	return &s
}

func nullUUID(value *string) (uuid.NullUUID, error) {
	if value == nil || *value == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func uuidPtr(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	return &value.UUID
}

func ts(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func tsp(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func mapUser(user db.User) meResponse {
	return meResponse{
		ID:        user.ID,
		Email:     stringPtr(user.Email),
		FirstName: stringPtr(user.FirstName),
		LastName:  stringPtr(user.LastName),
		ImageURL:  stringPtr(user.ImageUrl),
		CreatedAt: ts(user.CreatedAt),
		UpdatedAt: ts(user.UpdatedAt),
	}
}

func mapBusiness(b db.Business, includePrivate bool) businessResponse {
	out := businessResponse{
		ID:                b.ID,
		Name:              b.Name,
		Slug:              b.Slug,
		Description:       b.Description,
		BusinessType:      b.BusinessType,
		LogoURL:           stringPtr(b.LogoUrl),
		CoverImageURL:     stringPtr(b.CoverImageUrl),
		City:              b.City,
		State:             b.State,
		Country:           b.Country,
		Latitude:          decimalPtr(b.Latitude),
		Longitude:         decimalPtr(b.Longitude),
		PickupAvailable:   b.PickupAvailable,
		DeliveryAvailable: b.DeliveryAvailable,
		CreatedAt:         ts(b.CreatedAt),
		UpdatedAt:         ts(b.UpdatedAt),
	}
	if includePrivate {
		out.Phone = stringPtr(b.Phone)
		out.Whatsapp = stringPtr(b.Whatsapp)
		out.Email = stringPtr(b.Email)
		out.Website = stringPtr(b.Website)
		out.Status = b.Status
		out.Address = stringPtr(b.Address)
		out.Neighborhood = stringPtr(b.Neighborhood)
		out.PostalCode = stringPtr(b.PostalCode)
	}
	return out
}

func mapProduct(p db.Product, includePrivate bool) productResponse {
	out := productResponse{
		ID:          p.ID,
		CategoryID:  uuidPtr(p.CategoryID),
		Name:        p.Name,
		Slug:        p.Slug,
		Description: p.Description,
		Brand:       stringPtr(p.Brand),
		Unit:        p.Unit,
		ProductType: p.ProductType,
		IsHandmade:  p.IsHandmade,
	}
	if includePrivate {
		out.BusinessID = &p.BusinessID
		out.IsPublic = p.IsPublic
		out.Status = p.Status
		out.CreatedAt = tsp(p.CreatedAt)
		out.UpdatedAt = tsp(p.UpdatedAt)
	}
	return out
}

func mapVariant(v db.ProductVariant, includePrivate bool) variantResponse {
	out := variantResponse{
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
		out.Barcode = stringPtr(v.Barcode)
		out.InternalCode = v.InternalCode
		out.Cost = decimalPtr(v.Cost)
		out.TrackInventory = v.TrackInventory
		out.ReorderPoint = v.ReorderPoint.String()
		out.LeadTimeDays = v.LeadTimeDays
		out.Status = v.Status
		out.CreatedAt = tsp(v.CreatedAt)
		out.UpdatedAt = tsp(v.UpdatedAt)
	}
	return out
}
