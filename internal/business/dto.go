package business

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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
	Timezone          string  `json:"timezone"`
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
	Timezone          string    `json:"timezone"`
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
		Timezone:          b.Timezone,
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
	req.Timezone = api.FirstNonEmpty(v.Clean(req.Timezone), "America/Mexico_City")
	if err := v.StringLength(req.City, "city", 1, 120); err != nil {
		return db.CreateBusinessParams{}, err
	}
	if err := v.StringLength(req.State, "state", 1, 120); err != nil {
		return db.CreateBusinessParams{}, err
	}
	if len(req.Country) != 2 || req.Country[0] < 'A' || req.Country[0] > 'Z' || req.Country[1] < 'A' || req.Country[1] > 'Z' {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "country must be a two-letter code")
	}
	if len(req.Timezone) > 100 {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "timezone must be at most 100 characters")
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "timezone must be a valid IANA timezone")
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
		Timezone:          req.Timezone,
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
		Timezone:          params.Timezone,
	}, nil
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

func HoursParams(businessID uuid.UUID, req HoursRequest) ([]db.UpsertBusinessHourParams, error) {
	if len(req.Hours) > 7 {
		return nil, fiber.NewError(fiber.StatusBadRequest, "hours must contain at most seven days")
	}
	seen := make(map[int32]struct{}, len(req.Hours))
	params := make([]db.UpsertBusinessHourParams, 0, len(req.Hours))
	for _, hour := range req.Hours {
		if hour.DayOfWeek < 0 || hour.DayOfWeek > 6 {
			return nil, fiber.NewError(fiber.StatusBadRequest, "day_of_week must be between 0 and 6")
		}
		if _, exists := seen[hour.DayOfWeek]; exists {
			return nil, fiber.NewError(fiber.StatusBadRequest, "day_of_week must be unique")
		}
		seen[hour.DayOfWeek] = struct{}{}

		opensAt, err := parseBusinessTime(hour.OpensAt, "opens_at")
		if err != nil {
			return nil, err
		}
		closesAt, err := parseBusinessTime(hour.ClosesAt, "closes_at")
		if err != nil {
			return nil, err
		}
		if hour.IsClosed {
			if opensAt.Valid || closesAt.Valid {
				return nil, fiber.NewError(fiber.StatusBadRequest, "closed days must not have opening or closing times")
			}
		} else {
			if !opensAt.Valid || !closesAt.Valid {
				return nil, fiber.NewError(fiber.StatusBadRequest, "open days require opens_at and closes_at")
			}
			if opensAt.Microseconds == closesAt.Microseconds {
				return nil, fiber.NewError(fiber.StatusBadRequest, "opens_at and closes_at must be different")
			}
		}
		params = append(params, db.UpsertBusinessHourParams{
			BusinessID: businessID,
			DayOfWeek:  hour.DayOfWeek,
			OpensAt:    opensAt,
			ClosesAt:   closesAt,
			IsClosed:   hour.IsClosed,
		})
	}
	return params, nil
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

func parseBusinessTime(value *string, field string) (pgtype.Time, error) {
	if value == nil {
		return pgtype.Time{}, nil
	}
	cleaned := strings.TrimSpace(*value)
	parsed, err := time.Parse("15:04", cleaned)
	if err != nil {
		return pgtype.Time{}, fiber.NewError(fiber.StatusBadRequest, field+" must use HH:MM format")
	}
	microseconds := int64(parsed.Hour())*60*60*1_000_000 + int64(parsed.Minute())*60*1_000_000
	return pgtype.Time{Microseconds: microseconds, Valid: true}, nil
}

func formatBusinessTime(value pgtype.Time) *string {
	if !value.Valid {
		return nil
	}
	totalMinutes := value.Microseconds / 1_000_000 / 60
	formatted := fmt.Sprintf("%02d:%02d", totalMinutes/60, totalMinutes%60)
	return &formatted
}
