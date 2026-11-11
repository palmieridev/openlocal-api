package business

import (
	"time"

	"github.com/google/uuid"
)

type Request struct {
	Name              string                `json:"name"`
	Slug              string                `json:"slug"`
	Description       string                `json:"description"`
	BusinessType      string                `json:"business_type"`
	Phone             *string               `json:"phone"`
	Whatsapp          *string               `json:"whatsapp"`
	Email             *string               `json:"email"`
	Website           *string               `json:"website"`
	LogoURL           *string               `json:"logo_url"`
	CoverImageURL     *string               `json:"cover_image_url"`
	Status            string                `json:"status"`
	Address           *string               `json:"address"`
	Neighborhood      *string               `json:"neighborhood"`
	City              string                `json:"city"`
	State             string                `json:"state"`
	Country           string                `json:"country"`
	PostalCode        *string               `json:"postal_code"`
	Latitude          *string               `json:"latitude"`
	Longitude         *string               `json:"longitude"`
	PickupAvailable   bool                  `json:"pickup_available"`
	DeliveryAvailable bool                  `json:"delivery_available"`
	Timezone          string                `json:"timezone"`
	LocationMode      *string               `json:"location_mode"`
	ServiceAreas      *[]ServiceAreaRequest `json:"service_areas"`
}

type ServiceAreaRequest struct {
	Name         string  `json:"name"`
	Country      string  `json:"country"`
	State        string  `json:"state"`
	Municipality *string `json:"municipality"`
	City         *string `json:"city"`
	Neighborhood *string `json:"neighborhood"`
	PostalCode   *string `json:"postal_code"`
}

type ServiceAreaResponse struct {
	Name         string  `json:"name"`
	Country      string  `json:"country"`
	State        string  `json:"state"`
	Municipality *string `json:"municipality,omitempty"`
	City         *string `json:"city,omitempty"`
	Neighborhood *string `json:"neighborhood,omitempty"`
	PostalCode   *string `json:"postal_code,omitempty"`
}

type Response struct {
	ID                uuid.UUID             `json:"id"`
	ClerkOrgID        string                `json:"clerk_org_id,omitempty"`
	Name              string                `json:"name"`
	Slug              string                `json:"slug"`
	Description       string                `json:"description"`
	BusinessType      string                `json:"business_type"`
	Phone             *string               `json:"phone,omitempty"`
	Whatsapp          *string               `json:"whatsapp,omitempty"`
	Email             *string               `json:"email,omitempty"`
	Website           *string               `json:"website,omitempty"`
	LogoURL           *string               `json:"logo_url,omitempty"`
	CoverImageURL     *string               `json:"cover_image_url,omitempty"`
	Status            string                `json:"status,omitempty"`
	Address           *string               `json:"address,omitempty"`
	Neighborhood      *string               `json:"neighborhood,omitempty"`
	City              *string               `json:"city,omitempty"`
	State             *string               `json:"state,omitempty"`
	Country           *string               `json:"country,omitempty"`
	PostalCode        *string               `json:"postal_code,omitempty"`
	Latitude          *string               `json:"latitude,omitempty"`
	Longitude         *string               `json:"longitude,omitempty"`
	PickupAvailable   bool                  `json:"pickup_available"`
	DeliveryAvailable bool                  `json:"delivery_available"`
	Timezone          string                `json:"timezone"`
	LocationMode      string                `json:"location_mode"`
	ServiceAreas      []ServiceAreaResponse `json:"service_areas"`
	CreatedAt         time.Time             `json:"created_at,omitempty"`
	UpdatedAt         time.Time             `json:"updated_at,omitempty"`
}

type HoursRequest struct {
	Hours []HourRequest `json:"hours"`
}

type HourRequest struct {
	DayOfWeek int32   `json:"day_of_week"`
	OpensAt   *string `json:"opens_at"`
	ClosesAt  *string `json:"closes_at"`
	IsClosed  bool    `json:"is_closed"`
}

type HoursResponse struct {
	Timezone string         `json:"timezone"`
	Hours    []HourResponse `json:"hours"`
}

type HourResponse struct {
	DayOfWeek int32   `json:"day_of_week"`
	OpensAt   *string `json:"opens_at"`
	ClosesAt  *string `json:"closes_at"`
	IsClosed  bool    `json:"is_closed"`
}
