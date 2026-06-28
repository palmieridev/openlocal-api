package business

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Request struct {
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

type Response struct {
	ID                uuid.UUID `json:"id"`
	ClerkOrgID        string    `json:"clerk_org_id,omitempty"`
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

func Map(b db.Business, includePrivate bool) Response {
	out := Response{
		ID:                b.ID,
		Name:              b.Name,
		Slug:              b.Slug,
		Description:       b.Description,
		BusinessType:      b.BusinessType,
		LogoURL:           api.StringPtr(b.LogoUrl),
		CoverImageURL:     api.StringPtr(b.CoverImageUrl),
		City:              b.City,
		State:             b.State,
		Country:           b.Country,
		Latitude:          api.DecimalPtr(b.Latitude),
		Longitude:         api.DecimalPtr(b.Longitude),
		PickupAvailable:   b.PickupAvailable,
		DeliveryAvailable: b.DeliveryAvailable,
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

func CreateParams(req Request) (db.CreateBusinessParams, error) {
	req.Name = v.Clean(req.Name)
	req.Slug = v.Slug(api.FirstNonEmpty(req.Slug, req.Name))
	if req.Name == "" || len(req.Name) > 160 {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "name is required and must be <= 160 characters")
	}
	if err := v.ValidateSlug(req.Slug); err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	req.Description = v.Clean(req.Description)
	req.BusinessType = api.FirstNonEmpty(v.Slug(req.BusinessType), "retail")
	req.Status = api.FirstNonEmpty(v.Slug(req.Status), "draft")
	req.City = api.FirstNonEmpty(v.Clean(req.City), "CDMX")
	req.State = api.FirstNonEmpty(v.Clean(req.State), "CDMX")
	req.Country = strings.ToUpper(api.FirstNonEmpty(v.Clean(req.Country), "MX"))
	lat, err := api.NullDecimal(req.Latitude)
	if err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "latitude must be a decimal")
	}
	lng, err := api.NullDecimal(req.Longitude)
	if err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "longitude must be a decimal")
	}
	return db.CreateBusinessParams{
		Name:              req.Name,
		Slug:              req.Slug,
		Description:       req.Description,
		BusinessType:      req.BusinessType,
		Phone:             api.NullString(v.CleanOptional(req.Phone)),
		Whatsapp:          api.NullString(v.CleanOptional(req.Whatsapp)),
		Email:             api.NullString(v.CleanOptional(req.Email)),
		Website:           api.NullString(v.CleanOptional(req.Website)),
		LogoUrl:           api.NullString(v.CleanOptional(req.LogoURL)),
		CoverImageUrl:     api.NullString(v.CleanOptional(req.CoverImageURL)),
		Status:            req.Status,
		Address:           api.NullString(v.CleanOptional(req.Address)),
		Neighborhood:      api.NullString(v.CleanOptional(req.Neighborhood)),
		City:              req.City,
		State:             req.State,
		Country:           req.Country,
		PostalCode:        api.NullString(v.CleanOptional(req.PostalCode)),
		Latitude:          lat,
		Longitude:         lng,
		PickupAvailable:   req.PickupAvailable,
		DeliveryAvailable: req.DeliveryAvailable,
	}, nil
}

func UpdateParams(id, userID uuid.UUID, req Request) (db.UpdateBusinessParams, error) {
	params, err := CreateParams(req)
	if err != nil {
		return db.UpdateBusinessParams{}, err
	}
	return db.UpdateBusinessParams{
		ID:                id,
		UserID:            userID,
		Name:              params.Name,
		Description:       params.Description,
		BusinessType:      params.BusinessType,
		Phone:             params.Phone,
		Whatsapp:          params.Whatsapp,
		Email:             params.Email,
		Website:           params.Website,
		LogoUrl:           params.LogoUrl,
		CoverImageUrl:     params.CoverImageUrl,
		Status:            params.Status,
		Address:           params.Address,
		Neighborhood:      params.Neighborhood,
		City:              params.City,
		State:             params.State,
		Country:           params.Country,
		PostalCode:        params.PostalCode,
		Latitude:          params.Latitude,
		Longitude:         params.Longitude,
		PickupAvailable:   params.PickupAvailable,
		DeliveryAvailable: params.DeliveryAvailable,
	}, nil
}
