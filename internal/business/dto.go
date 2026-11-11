package business

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
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

func Map(b db.Business, includePrivate bool, areas []db.BusinessServiceArea) Response {
	out := Response{
		ID:                b.ID,
		Name:              b.Name,
		Slug:              b.Slug,
		Description:       b.Description,
		BusinessType:      b.BusinessType,
		LogoURL:           api.StringPtr(b.LogoUrl),
		CoverImageURL:     api.StringPtr(b.CoverImageUrl),
		City:              api.StringPtr(b.City),
		State:             api.StringPtr(b.State),
		Country:           api.StringPtr(b.Country),
		Latitude:          api.DecimalPtr(b.Latitude),
		Longitude:         api.DecimalPtr(b.Longitude),
		PickupAvailable:   b.PickupAvailable,
		DeliveryAvailable: b.DeliveryAvailable,
		Timezone:          b.Timezone,
		LocationMode:      b.LocationMode,
		ServiceAreas:      MapServiceAreas(areas),
		CreatedAt:         api.TS(b.CreatedAt),
		UpdatedAt:         api.TS(b.UpdatedAt),
	}
	if includePrivate {
		if b.ClerkOrgID.Valid {
			out.ClerkOrgID = b.ClerkOrgID.String
		}
		out.Phone = api.StringPtr(b.Phone)
		out.Whatsapp = api.StringPtr(b.Whatsapp)
		out.Email = api.StringPtr(b.Email)
		out.Website = api.StringPtr(b.Website)
		out.Status = b.Status
		out.Address = api.StringPtr(b.Address)
		out.Neighborhood = api.StringPtr(b.Neighborhood)
		out.PostalCode = api.StringPtr(b.PostalCode)
	}
	return out
}

func MapServiceAreas(rows []db.BusinessServiceArea) []ServiceAreaResponse {
	areas := make([]ServiceAreaResponse, 0, len(rows))
	for _, row := range rows {
		areas = append(areas, ServiceAreaResponse{
			Name: row.Name, Country: row.Country, State: row.State,
			Municipality: api.StringPtr(row.Municipality), City: api.StringPtr(row.City),
			Neighborhood: api.StringPtr(row.Neighborhood), PostalCode: api.StringPtr(row.PostalCode),
		})
	}
	return areas
}

func GroupServiceAreasByBusiness(rows []db.BusinessServiceArea) map[uuid.UUID][]db.BusinessServiceArea {
	grouped := make(map[uuid.UUID][]db.BusinessServiceArea)
	for _, row := range rows {
		grouped[row.BusinessID] = append(grouped[row.BusinessID], row)
	}
	return grouped
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

// PublicResponse is the public projection of a business. It embeds Response so
// public payloads keep exactly the same field names, and adds the weekly
// opening hours that shoppers need to work out whether a business is open.
//
// Open/closed is deliberately NOT computed here: a cached or server-rendered
// answer goes stale the moment a business opens or closes, so clients compute
// it from these hours plus the business timezone.
type PublicResponse struct {
	Response
	Hours []HourResponse `json:"hours"`
}

// MapHourResponses projects hour rows. It always returns a non-nil slice so a
// business with no hours set serialises as `[]` rather than `null`.
//
// day_of_week is 0=Monday .. 6=Sunday.
func MapHourResponses(rows []db.BusinessHour) []HourResponse {
	hours := make([]HourResponse, 0, len(rows))
	for _, row := range rows {
		hours = append(hours, HourResponse{
			DayOfWeek: row.DayOfWeek,
			OpensAt:   formatBusinessTime(row.OpensAt),
			ClosesAt:  formatBusinessTime(row.ClosesAt),
			IsClosed:  row.IsClosed,
		})
	}
	return hours
}

// GroupHoursByBusiness buckets batch-fetched hour rows by business id, for
// listings that load every business's hours in one query.
func GroupHoursByBusiness(rows []db.BusinessHour) map[uuid.UUID][]HourResponse {
	grouped := make(map[uuid.UUID][]HourResponse)
	for _, row := range rows {
		grouped[row.BusinessID] = append(grouped[row.BusinessID], HourResponse{
			DayOfWeek: row.DayOfWeek,
			OpensAt:   formatBusinessTime(row.OpensAt),
			ClosesAt:  formatBusinessTime(row.ClosesAt),
			IsClosed:  row.IsClosed,
		})
	}
	return grouped
}

func MapHours(timezone string, rows []db.BusinessHour) HoursResponse {
	return HoursResponse{Timezone: timezone, Hours: MapHourResponses(rows)}
}

func formatBusinessTime(value pgtype.Time) *string {
	if !value.Valid {
		return nil
	}
	totalMinutes := value.Microseconds / 1_000_000 / 60
	formatted := fmt.Sprintf("%02d:%02d", totalMinutes/60, totalMinutes%60)
	return &formatted
}
