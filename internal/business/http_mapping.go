package business

import (
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	"github.com/shopspring/decimal"
)

func profileInput(r Request) profile.Input {
	out := profile.Input{Name: r.Name,
		Slug:              r.Slug,
		Description:       r.Description,
		BusinessType:      r.BusinessType,
		Phone:             r.Phone,
		Whatsapp:          r.Whatsapp,
		Email:             r.Email,
		Website:           r.Website,
		LogoURL:           r.LogoURL,
		CoverImageURL:     r.CoverImageURL,
		Status:            r.Status,
		Address:           r.Address,
		Neighborhood:      r.Neighborhood,
		City:              r.City,
		State:             r.State,
		Country:           r.Country,
		PostalCode:        r.PostalCode,
		Latitude:          r.Latitude,
		Longitude:         r.Longitude,
		PickupAvailable:   r.PickupAvailable,
		DeliveryAvailable: r.DeliveryAvailable,
		Timezone:          r.Timezone,
		LocationMode:      r.LocationMode}
	if r.ServiceAreas != nil {
		areas := make([]profile.AreaInput, 0, len(*r.ServiceAreas))
		for _, a := range *r.ServiceAreas {
			areas = append(areas, profile.AreaInput{Name: a.Name, Country: a.Country, State: a.State, Municipality: a.Municipality, City: a.City, Neighborhood: a.Neighborhood, PostalCode: a.PostalCode})
		}
		out.ServiceAreas = &areas
	}
	return out
}
func decimalString(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}
func profileResponse(b profile.Business) Response {
	out := Response{ID: b.ID, ClerkOrgID: b.OrganizationID, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Latitude: decimalString(b.Latitude), Longitude: decimalString(b.Longitude), ServiceAreas: []ServiceAreaResponse{}, Name: b.Name,
		Slug:              b.Slug,
		Description:       b.Description,
		BusinessType:      b.BusinessType,
		Phone:             b.Phone,
		Whatsapp:          b.Whatsapp,
		Email:             b.Email,
		Website:           b.Website,
		LogoURL:           b.LogoURL,
		CoverImageURL:     b.CoverImageURL,
		Status:            b.Status,
		Address:           b.Address,
		Neighborhood:      b.Neighborhood,
		City:              b.City,
		State:             b.State,
		Country:           b.Country,
		PostalCode:        b.PostalCode,
		PickupAvailable:   b.PickupAvailable,
		DeliveryAvailable: b.DeliveryAvailable,
		Timezone:          b.Timezone,
		LocationMode:      b.LocationMode}
	for _, a := range b.Areas {
		out.ServiceAreas = append(out.ServiceAreas, ServiceAreaResponse{Name: a.Name, Country: a.Country, State: a.State, Municipality: a.Municipality, City: a.City, Neighborhood: a.Neighborhood, PostalCode: a.PostalCode})
	}
	return out
}
func hourInputs(r HoursRequest) []profile.Hour {
	out := make([]profile.Hour, 0, len(r.Hours))
	for _, h := range r.Hours {
		out = append(out, profile.Hour{DayOfWeek: h.DayOfWeek, OpensAt: h.OpensAt, ClosesAt: h.ClosesAt, IsClosed: h.IsClosed})
	}
	return out
}
func scheduleResponse(r profile.Schedule) HoursResponse {
	out := HoursResponse{Timezone: r.Timezone, Hours: []HourResponse{}}
	for _, h := range r.Hours {
		out.Hours = append(out.Hours, HourResponse{DayOfWeek: h.DayOfWeek, OpensAt: h.OpensAt, ClosesAt: h.ClosesAt, IsClosed: h.IsClosed})
	}
	return out
}
