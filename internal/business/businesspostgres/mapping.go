package businesspostgres

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func nullDecimal(value *decimal.Decimal) decimal.NullDecimal {
	if value == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: *value, Valid: true}
}
func decimalPtr(value decimal.NullDecimal) *decimal.Decimal {
	if !value.Valid {
		return nil
	}
	return &value.Decimal
}
func nonempty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func createParams(d profile.Data) db.CreateBusinessParams {
	return db.CreateBusinessParams{
		Name:              d.Name,
		Slug:              d.Slug,
		Description:       d.Description,
		BusinessType:      d.BusinessType,
		Phone:             api.NullString(d.Phone),
		Whatsapp:          api.NullString(d.Whatsapp),
		Email:             api.NullString(d.Email),
		Website:           api.NullString(d.Website),
		LogoUrl:           api.NullString(d.LogoURL),
		CoverImageUrl:     api.NullString(d.CoverImageURL),
		Status:            d.Status,
		Address:           api.NullString(d.Address),
		Neighborhood:      api.NullString(d.Neighborhood),
		City:              api.NullString(d.City),
		State:             api.NullString(d.State),
		Country:           api.NullString(d.Country),
		PostalCode:        api.NullString(d.PostalCode),
		Latitude:          nullDecimal(d.Latitude),
		Longitude:         nullDecimal(d.Longitude),
		PickupAvailable:   d.PickupAvailable,
		DeliveryAvailable: d.DeliveryAvailable,
		Timezone:          d.Timezone,
		LocationMode:      d.LocationMode,
	}
}
func updateParams(id profile.Identity, d profile.Data) db.UpdateBusinessParams {
	params := createParams(d)
	return db.UpdateBusinessParams{
		ID:                id.BusinessID,
		UserID:            id.ActorID,
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
		LocationMode:      params.LocationMode,
	}
}
func areaParams(businessID uuid.UUID, input profile.Area) db.CreateBusinessServiceAreaParams {
	return db.CreateBusinessServiceAreaParams{
		BusinessID: businessID, Name: input.Name, Country: input.Country, State: input.State,
		Municipality: api.NullString(input.Municipality), City: api.NullString(input.City),
		Neighborhood: api.NullString(input.Neighborhood), PostalCode: api.NullString(input.PostalCode),
		CountryKey: input.CountryKey, StateKey: input.StateKey,
		MunicipalityKey: api.NullString(input.MunicipalityKey), CityKey: api.NullString(input.CityKey),
		NeighborhoodKey: api.NullString(input.NeighborhoodKey), PostalCodeKey: api.NullString(nonempty(input.PostalCodeKey)),
		NormalizedKey: input.NormalizedKey,
	}
}
func mapBusiness(b db.Business, areas []db.BusinessServiceArea) profile.Business {
	out := profile.Business{ID: b.ID, OrganizationID: b.ClerkOrgID.String, CreatedAt: api.TS(b.CreatedAt), UpdatedAt: api.TS(b.UpdatedAt), Data: profile.Data{
		Name: b.Name, Slug: b.Slug, Description: b.Description, BusinessType: b.BusinessType, Status: b.Status, Timezone: b.Timezone, LocationMode: b.LocationMode,
		Phone: api.StringPtr(b.Phone), Whatsapp: api.StringPtr(b.Whatsapp), Email: api.StringPtr(b.Email), Website: api.StringPtr(b.Website), LogoURL: api.StringPtr(b.LogoUrl), CoverImageURL: api.StringPtr(b.CoverImageUrl),
		Address: api.StringPtr(b.Address), Neighborhood: api.StringPtr(b.Neighborhood), City: api.StringPtr(b.City), State: api.StringPtr(b.State), Country: api.StringPtr(b.Country), PostalCode: api.StringPtr(b.PostalCode), Latitude: decimalPtr(b.Latitude), Longitude: decimalPtr(b.Longitude), PickupAvailable: b.PickupAvailable, DeliveryAvailable: b.DeliveryAvailable, Areas: []profile.Area{},
	}}
	for _, a := range areas {
		out.Areas = append(out.Areas, profile.Area{Name: a.Name, Country: a.Country, State: a.State, Municipality: api.StringPtr(a.Municipality), City: api.StringPtr(a.City), Neighborhood: api.StringPtr(a.Neighborhood), PostalCode: api.StringPtr(a.PostalCode), CountryKey: a.CountryKey, StateKey: a.StateKey, MunicipalityKey: api.StringPtr(a.MunicipalityKey), CityKey: api.StringPtr(a.CityKey), NeighborhoodKey: api.StringPtr(a.NeighborhoodKey), PostalCodeKey: a.PostalCodeKey.String, NormalizedKey: a.NormalizedKey})
	}
	return out
}
func clock(value *string) pgtype.Time {
	if value == nil {
		return pgtype.Time{}
	}
	t, _ := time.Parse("15:04", *value)
	return pgtype.Time{Microseconds: int64(t.Hour()*60+t.Minute()) * 60 * 1000000, Valid: true}
}
func clockString(value pgtype.Time) *string {
	if !value.Valid {
		return nil
	}
	minutes := value.Microseconds / 1000000 / 60
	s := fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
	return &s
}
func mapHours(rows []db.BusinessHour) []profile.Hour {
	out := make([]profile.Hour, 0, len(rows))
	for _, r := range rows {
		out = append(out, profile.Hour{DayOfWeek: r.DayOfWeek, OpensAt: clockString(r.OpensAt), ClosesAt: clockString(r.ClosesAt), IsClosed: r.IsClosed})
	}
	return out
}
