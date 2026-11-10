package management

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ErrNotFound = errors.New("not found")

// Identity must be authorized by the caller boundary; request bodies cannot
// supply the actor. The service still checks every referenced resource's tenant.
type Identity struct{ BusinessID, ActorID uuid.UUID }

type ProductInput struct {
	CategoryID              *string
	Name, Slug, Description string
	Brand                   *string
	Unit, ProductType       string
	IsHandmade, IsPublic    bool
	Status                  string
}

type ProductData struct {
	CategoryID              *uuid.UUID
	Name, Slug, Description string
	Brand                   *string
	Unit, ProductType       string
	IsHandmade, IsPublic    bool
	Status                  string
}

type Product struct {
	ProductData
	ID, BusinessID       uuid.UUID
	CreatedAt, UpdatedAt time.Time
}

type VariantInput struct {
	ProductID, SKU         string
	Barcode                *string
	InternalCode, Name     string
	Description, PriceNote *string
	Attributes             map[string]any
	ImageURL               *string
	Price                  string
	Cost                   *string
	Currency               string
	TrackInventory         bool
	PublicStockStatus      string
	IsPublic               *bool
	ReorderPoint           string
	LeadTimeDays           int32
	Status                 string
}

type VariantData struct {
	ProductID              uuid.UUID
	SKU                    string
	Barcode                *string
	InternalCode, Name     string
	Description, PriceNote *string
	Attributes             []byte
	Price                  decimal.Decimal
	Cost                   *decimal.Decimal
	Currency               string
	TrackInventory         bool
	PublicStockStatus      string
	IsPublic               *bool
	ReorderPoint           decimal.Decimal
	LeadTimeDays           int32
	Status                 string
}

type Variant struct {
	VariantData
	ID, BusinessID       uuid.UUID
	ImageURL             *string
	CreatedAt, UpdatedAt time.Time
}

// PublicProduct is a deliberately limited result. Costs, tenant identities,
// exact inventory levels and attributes cannot cross the public reader port.
type PublicProduct struct {
	ID, VariantID                       uuid.UUID
	Name, Slug, Description             string
	Brand                               *string
	Unit, ProductType, SKU, VariantName string
	VariantDescription, PriceNote       *string
	Price                               decimal.Decimal
	Currency, PublicStockStatus         string
	ImageURL                            *string
}

type Page struct{ Limit, Offset int32 }

type Products interface {
	RequireCategory(context.Context, uuid.UUID, uuid.UUID) error
	CreateProduct(context.Context, uuid.UUID, ProductData) (Product, error)
	UpdateProduct(context.Context, uuid.UUID, uuid.UUID, ProductData) (Product, error)
	Product(context.Context, uuid.UUID, uuid.UUID) (Product, error)
	Products(context.Context, uuid.UUID, Page) ([]Product, error)
	ArchiveProduct(context.Context, uuid.UUID, uuid.UUID) error
}

type Variants interface {
	Begin(context.Context) (VariantTransaction, error)
	Variant(context.Context, uuid.UUID, uuid.UUID) (Variant, error)
	Variants(context.Context, uuid.UUID, uuid.UUID, Page) ([]Variant, error)
	ByBarcode(context.Context, uuid.UUID, string) (Variant, error)
	BySKU(context.Context, uuid.UUID, string) (Variant, error)
	ArchiveVariant(context.Context, uuid.UUID, uuid.UUID) error
}

type PublicProducts interface {
	PublicProducts(context.Context, string, Page) ([]PublicProduct, error)
}

// VariantTransaction owns variant/image writes together. LockVariant serializes
// replacements so concurrent requests cannot leave multiple storefront images.
type VariantTransaction interface {
	RequireProduct(context.Context, uuid.UUID, uuid.UUID) error
	LockVariant(context.Context, uuid.UUID, uuid.UUID) (Variant, error)
	CreateVariant(context.Context, uuid.UUID, VariantData) (Variant, error)
	UpdateVariant(context.Context, uuid.UUID, uuid.UUID, VariantData) (Variant, error)
	ApplyImage(context.Context, uuid.UUID, *string) (*string, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}
