// Package discovery validates marketplace searches and exposes public-only read models.
package discovery

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	"github.com/shopspring/decimal"
)

type Business struct {
	ID                uuid.UUID
	Name              string
	Slug              string
	Description       string
	BusinessType      string
	Phone             *string
	Whatsapp          *string
	Email             *string
	Website           *string
	LogoURL           *string
	CoverImageURL     *string
	Address           *string
	Neighborhood      *string
	City              *string
	State             *string
	Country           *string
	PostalCode        *string
	Latitude          *string
	Longitude         *string
	PickupAvailable   bool
	DeliveryAvailable bool
	Timezone          string
	LocationMode      string
	ServiceAreas      []profile.AreaInput
	Hours             []profile.Hour
}

type Product struct {
	BusinessSlug       string
	BusinessName       string
	ID                 uuid.UUID
	Name               string
	Slug               string
	Description        string
	Brand              *string
	Unit               string
	ProductType        string
	VariantID          uuid.UUID
	SKU                string
	VariantName        string
	VariantDescription *string
	PriceNote          *string
	Price              string
	Currency           string
	PublicStockStatus  string
	ImageURL           *string
}

type Input struct {
	Query                                                        string
	MinLat, MaxLat, MinLng, MaxLng                               string
	Country, State, Municipality, City, Neighborhood, PostalCode string
	Limit, Offset                                                int32
}
type Filter struct {
	Query                                                                          string
	HasBBox, HasAreaFilter                                                         bool
	MinLat, MaxLat, MinLng, MaxLng                                                 *decimal.Decimal
	Country, State, Municipality, City, Neighborhood, PostalCode                   *string
	CountryKey, StateKey, MunicipalityKey, CityKey, NeighborhoodKey, PostalCodeKey *string
	Limit, Offset                                                                  int32
}

var ErrNotFound = errors.New("not found")

type Store interface {
	Business(context.Context, string) (Business, error)
	Businesses(context.Context, Filter) ([]Business, error)
	Products(context.Context, Filter) ([]Product, error)
}
