// Package profile owns business profiles, reach and weekly-hour rules.
package profile

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"time"
)

type Input struct {
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
	Status            string
	Address           *string
	Neighborhood      *string
	City              string
	State             string
	Country           string
	PostalCode        *string
	Latitude          *string
	Longitude         *string
	PickupAvailable   bool
	DeliveryAvailable bool
	Timezone          string
	LocationMode      *string
	ServiceAreas      *[]AreaInput
}

type AreaInput struct {
	Name         string
	Country      string
	State        string
	Municipality *string
	City         *string
	Neighborhood *string
	PostalCode   *string
}

type Area struct {
	Name, Country, State                         string
	Municipality, City, Neighborhood, PostalCode *string
	CountryKey, StateKey                         string
	MunicipalityKey, CityKey, NeighborhoodKey    *string
	PostalCodeKey, NormalizedKey                 string
}

type Data struct {
	Name, Slug, Description, BusinessType, Status, Timezone, LocationMode string
	Phone, Whatsapp, Email, Website, LogoURL, CoverImageURL               *string
	Address, Neighborhood, City, State, Country, PostalCode               *string
	Latitude, Longitude                                                   *decimal.Decimal
	PickupAvailable, DeliveryAvailable                                    bool
	Areas                                                                 []Area
}
type Business struct {
	Data
	ID                   uuid.UUID
	OrganizationID       string
	CreatedAt, UpdatedAt time.Time
}

// Identity is supplied only after caller authorization for this business.
type Identity struct{ BusinessID, ActorID uuid.UUID }
type Actor struct {
	ID         uuid.UUID
	ProviderID string
}
type Hour struct {
	DayOfWeek         int32
	OpensAt, ClosesAt *string
	IsClosed          bool
}
type Schedule struct {
	Timezone string
	Hours    []Hour
}

var ErrNotFound = errors.New("not found")
var ErrMembership = errors.New("business membership is required")
var ErrRole = errors.New("role is not allowed")
var ErrOrganization = errors.New("active Clerk organization is required")
var ErrOrganizationsUnavailable = errors.New("clerk organization client is not configured")
var ErrOrganizationCreate = errors.New("failed to create Clerk organization")

type Organizations interface {
	Create(context.Context, Actor, string, string) (string, error)
	Delete(context.Context, string) error
}
type Reader interface {
	Business(context.Context, Identity) (Business, error)
	ActiveBusiness(context.Context, uuid.UUID, string) (Business, error)
	Hours(context.Context, uuid.UUID) ([]Hour, error)
}
type Transaction interface {
	LockBusiness(context.Context, Identity) (Business, error)
	Create(context.Context, Actor, string, Data) (Business, error)
	Update(context.Context, Identity, Data, bool) (Business, error)
	ReplaceHours(context.Context, Identity, []Hour) ([]Hour, error)
}
type Store interface {
	Reader
	WithinTransaction(context.Context, func(Transaction) error) error
}
