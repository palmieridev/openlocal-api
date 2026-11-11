package discovery

import (
	"github.com/palmieridev/openlocal-api/internal/geography"
	"github.com/shopspring/decimal"
	"strings"
)

func Parse(in Input) (Filter, error) {
	filter := Filter{Limit: in.Limit, Offset: in.Offset, Query: clean(in.Query)}
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return filter, invalid("limit must be between 1 and 100")
	}
	if filter.Offset < 0 || filter.Offset > 2000000000 {
		return filter, invalid("offset must be between 0 and 2000000000")
	}
	if err := stringLength(filter.Query, "q", 0, 200); err != nil {
		return filter, err
	}
	var err error
	if filter.MinLat, err = optionalCoordinate(in.MinLat, "min_lat"); err != nil {
		return filter, err
	}
	if filter.MaxLat, err = optionalCoordinate(in.MaxLat, "max_lat"); err != nil {
		return filter, err
	}
	if filter.MinLng, err = optionalCoordinate(in.MinLng, "min_lng"); err != nil {
		return filter, err
	}
	if filter.MaxLng, err = optionalCoordinate(in.MaxLng, "max_lng"); err != nil {
		return filter, err
	}
	filter.HasBBox = filter.MinLat != nil || filter.MaxLat != nil || filter.MinLng != nil || filter.MaxLng != nil
	for _, coordinate := range []struct {
		value *decimal.Decimal
		name  string
		min   decimal.Decimal
		max   decimal.Decimal
	}{
		{filter.MinLat, "min_lat", decimal.NewFromInt(-90), decimal.NewFromInt(90)},
		{filter.MaxLat, "max_lat", decimal.NewFromInt(-90), decimal.NewFromInt(90)},
		{filter.MinLng, "min_lng", decimal.NewFromInt(-180), decimal.NewFromInt(180)},
		{filter.MaxLng, "max_lng", decimal.NewFromInt(-180), decimal.NewFromInt(180)},
	} {
		if coordinate.value != nil {
			if err := decimalRange(*coordinate.value, coordinate.name, coordinate.min, coordinate.max, 6); err != nil {
				return filter, err
			}
		}
	}
	if filter.MinLat != nil && filter.MaxLat != nil && filter.MinLat.GreaterThan(*filter.MaxLat) {
		return filter, invalid("min_lat must be <= max_lat")
	}
	if filter.MinLng != nil && filter.MaxLng != nil && filter.MinLng.GreaterThan(*filter.MaxLng) {
		return filter, invalid("min_lng must be <= max_lng")
	}

	type areaField struct {
		name  string
		value string
		max   int
		raw   **string
		key   **string
	}
	fields := []areaField{
		{"country", in.Country, 2, &filter.Country, &filter.CountryKey},
		{"state", in.State, 120, &filter.State, &filter.StateKey},
		{"municipality", in.Municipality, 120, &filter.Municipality, &filter.MunicipalityKey},
		{"city", in.City, 120, &filter.City, &filter.CityKey},
		{"neighborhood", in.Neighborhood, 120, &filter.Neighborhood, &filter.NeighborhoodKey},
		{"postal_code", in.PostalCode, 20, &filter.PostalCode, &filter.PostalCodeKey},
	}
	for _, field := range fields {
		value := clean(field.value)
		if value == "" {
			continue
		}
		if err := stringLength(value, field.name, 1, field.max); err != nil {
			return filter, err
		}
		if field.name == "country" {
			value = strings.ToUpper(value)
			if len(value) != 2 || value[0] < 'A' || value[0] > 'Z' || value[1] < 'A' || value[1] > 'Z' {
				return filter, invalid("country must be a two-letter code")
			}
		}
		if field.raw != nil {
			*field.raw = &value
		}
		key := geography.SearchKey(value)
		if field.name == "postal_code" {
			key = geography.PostalKey(value)
		}
		*field.key = nonempty(key)
		filter.HasAreaFilter = true
	}
	return filter, nil
}

func optionalCoordinate(value, field string) (*decimal.Decimal, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	d, err := parseDecimal(value, field)
	if err != nil {
		return nil, err
	}
	return &d, nil
}
func nonempty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
