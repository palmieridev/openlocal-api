package marketplace

import (
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/marketplace/discovery"
)

type ProductResponse struct {
	BusinessSlug       string    `json:"business_slug"`
	BusinessName       string    `json:"business_name"`
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	Slug               string    `json:"slug"`
	Description        string    `json:"description"`
	Brand              *string   `json:"brand,omitempty"`
	Unit               string    `json:"unit"`
	ProductType        string    `json:"product_type"`
	VariantID          uuid.UUID `json:"variant_id"`
	SKU                string    `json:"sku"`
	VariantName        string    `json:"variant_name"`
	VariantDescription *string   `json:"variant_description,omitempty"`
	PriceNote          *string   `json:"price_note,omitempty"`
	Price              string    `json:"price"`
	Currency           string    `json:"currency"`
	PublicStockStatus  string    `json:"public_stock_status"`
	ImageURL           *string   `json:"image_url,omitempty"`
}

type BusinessResponse struct {
	ID                uuid.UUID             `json:"id"`
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
	Hours             []HourResponse        `json:"hours"`
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

type HourResponse struct {
	DayOfWeek int32   `json:"day_of_week"`
	OpensAt   *string `json:"opens_at"`
	ClosesAt  *string `json:"closes_at"`
	IsClosed  bool    `json:"is_closed"`
}

func businessResponse(b discovery.Business) BusinessResponse {
	out := BusinessResponse{ID: b.ID, Name: b.Name, Slug: b.Slug, Description: b.Description, BusinessType: b.BusinessType, Phone: b.Phone, Whatsapp: b.Whatsapp, Email: b.Email, Website: b.Website, LogoURL: b.LogoURL, CoverImageURL: b.CoverImageURL, Address: b.Address, Neighborhood: b.Neighborhood, City: b.City, State: b.State, Country: b.Country, PostalCode: b.PostalCode, Latitude: b.Latitude, Longitude: b.Longitude, PickupAvailable: b.PickupAvailable, DeliveryAvailable: b.DeliveryAvailable, Timezone: b.Timezone, LocationMode: b.LocationMode, Hours: []HourResponse{}, ServiceAreas: []ServiceAreaResponse{}}
	for _, h := range b.Hours {
		out.Hours = append(out.Hours, HourResponse{DayOfWeek: h.DayOfWeek, OpensAt: h.OpensAt, ClosesAt: h.ClosesAt, IsClosed: h.IsClosed})
	}
	for _, a := range b.ServiceAreas {
		out.ServiceAreas = append(out.ServiceAreas, ServiceAreaResponse{Name: a.Name, Country: a.Country, State: a.State, Municipality: a.Municipality, City: a.City, Neighborhood: a.Neighborhood, PostalCode: a.PostalCode})
	}
	return out
}
func productResponses(rows []discovery.Product) []ProductResponse {
	out := make([]ProductResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, ProductResponse(r))
	}
	return out
}
