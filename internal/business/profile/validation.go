package profile

import (
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/palmieridev/openlocal-api/internal/geography"
	"github.com/shopspring/decimal"
)

func Parse(req Input) (Data, error) {
	req.Name = clean(req.Name)
	req.Slug = slug(firstNonEmpty(req.Slug, req.Name))
	if err := stringLength(req.Name, "name", 2, 160); err != nil {
		return Data{}, err
	}
	if err := validateSlug(req.Slug); err != nil {
		return Data{}, invalid(err.Error())
	}
	req.Description = clean(req.Description)
	if err := stringLength(req.Description, "description", 0, 4000); err != nil {
		return Data{}, err
	}
	req.BusinessType = firstNonEmpty(slug(req.BusinessType), "retail")
	if err := stringLength(req.BusinessType, "business_type", 2, 80); err != nil {
		return Data{}, err
	}
	status, err := enum(req.Status, "status", "draft", "draft", "active", "suspended", "archived")
	if err != nil {
		return Data{}, err
	}
	locationModeInput := ""
	if req.LocationMode != nil {
		locationModeInput = *req.LocationMode
	}
	locationMode, err := enum(locationModeInput, "location_mode", "fixed", "fixed", "mobile", "hybrid")
	if err != nil {
		return Data{}, err
	}
	serviceAreas := []AreaInput{}
	if req.ServiceAreas != nil {
		serviceAreas = *req.ServiceAreas
	}
	areas, err := NormalizeAreas(serviceAreas)
	if err != nil {
		return Data{}, err
	}

	city := cleanOptional(optionalString(req.City))
	state := cleanOptional(optionalString(req.State))
	country := cleanOptional(optionalString(strings.ToUpper(req.Country)))
	req.Timezone = firstNonEmpty(clean(req.Timezone), "America/Mexico_City")
	for _, field := range []struct {
		value *string
		name  string
	}{{city, "city"}, {state, "state"}} {
		if field.value != nil {
			if err := stringLength(*field.value, field.name, 1, 120); err != nil {
				return Data{}, err
			}
		}
	}
	if country != nil && (len(*country) != 2 || (*country)[0] < 'A' || (*country)[0] > 'Z' || (*country)[1] < 'A' || (*country)[1] > 'Z') {
		return Data{}, invalid("country must be a two-letter code")
	}
	if len(req.Timezone) > 100 {
		return Data{}, invalid("timezone must be at most 100 characters")
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return Data{}, invalid("timezone must be a valid IANA timezone")
	}

	phone := cleanOptional(req.Phone)
	whatsapp := cleanOptional(req.Whatsapp)
	email := cleanOptional(req.Email)
	website := cleanOptional(req.Website)
	logoURL := cleanOptional(req.LogoURL)
	coverImageURL := cleanOptional(req.CoverImageURL)
	address := cleanOptional(req.Address)
	neighborhood := cleanOptional(req.Neighborhood)
	postalCode := cleanOptional(req.PostalCode)
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
			if err := stringLength(*field.value, field.name, 1, field.max); err != nil {
				return Data{}, err
			}
		}
	}
	if email != nil {
		parsed, parseErr := mail.ParseAddress(*email)
		if parseErr != nil || !strings.EqualFold(parsed.Address, *email) || len(*email) > 254 {
			return Data{}, invalid("email must be valid")
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
				return Data{}, invalid(field.name + " must be a valid http or https URL")
			}
		}
	}

	lat, err := optionalDecimal(req.Latitude)
	if err != nil {
		return Data{}, invalid("latitude must be a decimal")
	}
	lng, err := optionalDecimal(req.Longitude)
	if err != nil {
		return Data{}, invalid("longitude must be a decimal")
	}
	if lat != nil {
		if err := decimalRange(*lat, "latitude", decimal.NewFromInt(-90), decimal.NewFromInt(90), 6); err != nil {
			return Data{}, err
		}
	}
	if lng != nil {
		if err := decimalRange(*lng, "longitude", decimal.NewFromInt(-180), decimal.NewFromInt(180), 6); err != nil {
			return Data{}, err
		}
	}
	if (lat != nil) != (lng != nil) {
		return Data{}, invalid("latitude and longitude must be provided together")
	}
	hasFixedLocation := lat != nil && lng != nil
	hasFixedText := address != nil || neighborhood != nil || city != nil || state != nil || country != nil || postalCode != nil
	switch locationMode {
	case "fixed":
		if len(serviceAreas) > 0 {
			return Data{}, invalid("fixed businesses must not define service_areas; use hybrid")
		}
		if status == "active" && !hasFixedLocation {
			return Data{}, invalid("active fixed businesses require latitude and longitude")
		}
	case "mobile":
		if hasFixedLocation || hasFixedText {
			return Data{}, invalid("mobile businesses must not define a fixed location")
		}
		if req.PickupAvailable {
			return Data{}, invalid("mobile businesses cannot offer store pickup")
		}
		if status == "active" && len(serviceAreas) == 0 {
			return Data{}, invalid("active mobile businesses require at least one service area")
		}
	case "hybrid":
		if status == "active" && (!hasFixedLocation || len(serviceAreas) == 0) {
			return Data{}, invalid("active hybrid businesses require coordinates and at least one service area")
		}
	}
	return Data{
		Name:              req.Name,
		Slug:              req.Slug,
		Description:       req.Description,
		BusinessType:      req.BusinessType,
		Phone:             phone,
		Whatsapp:          whatsapp,
		Email:             email,
		Website:           website,
		LogoURL:           logoURL,
		CoverImageURL:     coverImageURL,
		Status:            status,
		Address:           address,
		Neighborhood:      neighborhood,
		City:              city,
		State:             state,
		Country:           country,
		PostalCode:        postalCode,
		Latitude:          lat,
		Longitude:         lng,
		PickupAvailable:   req.PickupAvailable,
		DeliveryAvailable: req.DeliveryAvailable,
		Timezone:          req.Timezone,
		LocationMode:      locationMode,
		Areas:             areas,
	}, nil
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func keyPtr(value *string) *string {
	if value == nil {
		return nil
	}
	key := geography.SearchKey(*value)
	if key == "" {
		return nil
	}
	return &key
}

func NormalizeAreas(requests []AreaInput) ([]Area, error) {
	if len(requests) > 20 {
		return nil, invalid("service_areas must contain at most 20 areas")
	}
	inputs := make([]Area, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		name := clean(request.Name)
		country := strings.ToUpper(firstNonEmpty(clean(request.Country), "MX"))
		state := clean(request.State)
		if err := stringLength(name, "service_areas.name", 2, 120); err != nil {
			return nil, err
		}
		if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
			return nil, invalid("service_areas.country must be a two-letter code")
		}
		if err := stringLength(state, "service_areas.state", 1, 120); err != nil {
			return nil, err
		}
		municipality := cleanOptional(request.Municipality)
		city := cleanOptional(request.City)
		neighborhood := cleanOptional(request.Neighborhood)
		postalCode := cleanOptional(request.PostalCode)
		for _, field := range []struct {
			value *string
			name  string
			max   int
		}{
			{municipality, "service_areas.municipality", 120},
			{city, "service_areas.city", 120},
			{neighborhood, "service_areas.neighborhood", 120},
			{postalCode, "service_areas.postal_code", 20},
		} {
			if field.value != nil {
				if err := stringLength(*field.value, field.name, 1, field.max); err != nil {
					return nil, err
				}
			}
		}
		countryKey := geography.SearchKey(country)
		stateKey := geography.SearchKey(state)
		municipalityKey := keyPtr(municipality)
		cityKey := keyPtr(city)
		neighborhoodKey := keyPtr(neighborhood)
		postalCodeKey := ""
		if postalCode != nil {
			postalCodeKey = geography.PostalKey(*postalCode)
			if postalCodeKey == "" {
				return nil, invalid("service_areas.postal_code is invalid")
			}
		}
		parts := []string{countryKey, stateKey, stringValue(municipalityKey), stringValue(cityKey), stringValue(neighborhoodKey), postalCodeKey}
		normalizedKey := strings.Join(parts, "|")
		if _, exists := seen[normalizedKey]; exists {
			return nil, invalid("service_areas must be unique")
		}
		seen[normalizedKey] = struct{}{}
		inputs = append(inputs, Area{
			Name: name, Country: country, State: state,
			Municipality: municipality, City: city, Neighborhood: neighborhood, PostalCode: postalCode,
			CountryKey: countryKey, StateKey: stateKey, MunicipalityKey: municipalityKey,
			CityKey: cityKey, NeighborhoodKey: neighborhoodKey, PostalCodeKey: postalCodeKey,
			NormalizedKey: normalizedKey,
		})
	}
	return inputs, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ParseHours(hours []Hour) ([]Hour, error) {
	if len(hours) > 7 {
		return nil, invalid("hours must contain at most seven days")
	}
	seen := make(map[int32]struct{}, len(hours))
	params := make([]Hour, 0, len(hours))
	for _, hour := range hours {
		if hour.DayOfWeek < 0 || hour.DayOfWeek > 6 {
			return nil, invalid("day_of_week must be between 0 and 6")
		}
		if _, exists := seen[hour.DayOfWeek]; exists {
			return nil, invalid("day_of_week must be unique")
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
			if opensAt != nil || closesAt != nil {
				return nil, invalid("closed days must not have opening or closing times")
			}
		} else {
			if opensAt == nil || closesAt == nil {
				return nil, invalid("open days require opens_at and closes_at")
			}
			if *opensAt == *closesAt {
				return nil, invalid("opens_at and closes_at must be different")
			}
		}
		params = append(params, Hour{

			DayOfWeek: hour.DayOfWeek,
			OpensAt:   opensAt,
			ClosesAt:  closesAt,
			IsClosed:  hour.IsClosed,
		})
	}
	return params, nil
}

func parseBusinessTime(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	cleaned := strings.TrimSpace(*value)
	parsed, err := time.Parse("15:04", cleaned)
	if err != nil {
		return nil, invalid(field + " must use HH:MM format")
	}
	formatted := parsed.Format("15:04")
	return &formatted, nil
}
