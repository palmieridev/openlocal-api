package business

import (
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/shopspring/decimal"
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
	if err := v.StringLength(req.Name, "name", 2, 160); err != nil {
		return db.CreateBusinessParams{}, err
	}
	if err := v.ValidateSlug(req.Slug); err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	req.Description = v.Clean(req.Description)
	if err := v.StringLength(req.Description, "description", 0, 4000); err != nil {
		return db.CreateBusinessParams{}, err
	}
	req.BusinessType = api.FirstNonEmpty(v.Slug(req.BusinessType), "retail")
	if err := v.StringLength(req.BusinessType, "business_type", 2, 80); err != nil {
		return db.CreateBusinessParams{}, err
	}
	status, err := v.Enum(req.Status, "status", "draft", "draft", "active", "suspended", "archived")
	if err != nil {
		return db.CreateBusinessParams{}, err
	}
	req.City = api.FirstNonEmpty(v.Clean(req.City), "CDMX")
	req.State = api.FirstNonEmpty(v.Clean(req.State), "CDMX")
	req.Country = strings.ToUpper(api.FirstNonEmpty(v.Clean(req.Country), "MX"))
	if err := v.StringLength(req.City, "city", 1, 120); err != nil {
		return db.CreateBusinessParams{}, err
	}
	if err := v.StringLength(req.State, "state", 1, 120); err != nil {
		return db.CreateBusinessParams{}, err
	}
	if len(req.Country) != 2 || req.Country[0] < 'A' || req.Country[0] > 'Z' || req.Country[1] < 'A' || req.Country[1] > 'Z' {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "country must be a two-letter code")
	}

	phone := v.CleanOptional(req.Phone)
	whatsapp := v.CleanOptional(req.Whatsapp)
	email := v.CleanOptional(req.Email)
	website := v.CleanOptional(req.Website)
	logoURL := v.CleanOptional(req.LogoURL)
	coverImageURL := v.CleanOptional(req.CoverImageURL)
	address := v.CleanOptional(req.Address)
	neighborhood := v.CleanOptional(req.Neighborhood)
	postalCode := v.CleanOptional(req.PostalCode)
	for _, field := range []struct {
		value *string
		name  string
		max   int
	}{
		{phone, "phone", 40},
		{whatsapp, "whatsapp", 40},
		{address, "address", 300},
		{neighborhood, "neighborhood", 120},
		{postalCode, "postal_code", 20},
	} {
		if field.value != nil {
			if err := v.StringLength(*field.value, field.name, 1, field.max); err != nil {
				return db.CreateBusinessParams{}, err
			}
		}
	}
	if email != nil {
		parsed, parseErr := mail.ParseAddress(*email)
		if parseErr != nil || !strings.EqualFold(parsed.Address, *email) || len(*email) > 254 {
			return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "email must be valid")
		}
		lower := strings.ToLower(*email)
		email = &lower
	}
	for _, field := range []struct {
		value *string
		name  string
	}{
		{website, "website"},
		{logoURL, "logo_url"},
		{coverImageURL, "cover_image_url"},
	} {
		if field.value != nil {
			parsed, parseErr := url.ParseRequestURI(*field.value)
			if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || len(*field.value) > 2048 {
				return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, field.name+" must be a valid http or https URL")
			}
		}
	}

	lat, err := api.NullDecimal(req.Latitude)
	if err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "latitude must be a decimal")
	}
	lng, err := api.NullDecimal(req.Longitude)
	if err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "longitude must be a decimal")
	}
	if lat.Valid {
		if err := v.DecimalRange(lat.Decimal, "latitude", decimal.NewFromInt(-90), decimal.NewFromInt(90), 6); err != nil {
			return db.CreateBusinessParams{}, err
		}
	}
	if lng.Valid {
		if err := v.DecimalRange(lng.Decimal, "longitude", decimal.NewFromInt(-180), decimal.NewFromInt(180), 6); err != nil {
			return db.CreateBusinessParams{}, err
		}
	}
	return db.CreateBusinessParams{
		Name:              req.Name,
		Slug:              req.Slug,
		Description:       req.Description,
		BusinessType:      req.BusinessType,
		Phone:             api.NullString(phone),
		Whatsapp:          api.NullString(whatsapp),
		Email:             api.NullString(email),
		Website:           api.NullString(website),
		LogoUrl:           api.NullString(logoURL),
		CoverImageUrl:     api.NullString(coverImageURL),
		Status:            status,
		Address:           api.NullString(address),
		Neighborhood:      api.NullString(neighborhood),
		City:              req.City,
		State:             req.State,
		Country:           req.Country,
		PostalCode:        api.NullString(postalCode),
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
