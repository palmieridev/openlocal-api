package marketplace

import (
	"database/sql"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/business"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/shopspring/decimal"
)

type Handler struct {
	rt api.Runtime
}

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt}
}

func (h Handler) RegisterPublicRoutes(apiGroup fiber.Router) {
	apiGroup.Get("/marketplace/search", h.searchMarketplaceProducts)
	apiGroup.Get("/marketplace/businesses", h.listPublicBusinesses)
	apiGroup.Get("/marketplace/products", h.searchMarketplaceProducts)
	apiGroup.Get("/public/businesses/:slug", h.getPublicBusiness)
}

func (h Handler) getPublicBusiness(c *fiber.Ctx) error {
	slug := v.Slug(c.Params("slug"))
	if err := v.ValidateSlug(slug); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	row, err := h.rt.Q.GetPublicBusinessBySlug(c.Context(), slug)
	if err != nil {
		return err
	}
	hourRows, err := h.rt.Q.ListBusinessHours(c.Context(), row.ID)
	if err != nil {
		return err
	}
	areaRows, err := h.rt.Q.ListBusinessServiceAreas(c.Context(), row.ID)
	if err != nil {
		return err
	}
	return c.JSON(business.PublicResponse{
		Response: business.Response{
			ID:                row.ID,
			Name:              row.Name,
			Slug:              row.Slug,
			Description:       row.Description,
			BusinessType:      row.BusinessType,
			Phone:             api.StringPtr(row.Phone),
			Whatsapp:          api.StringPtr(row.Whatsapp),
			Email:             api.StringPtr(row.Email),
			Website:           api.StringPtr(row.Website),
			LogoURL:           api.StringPtr(row.LogoUrl),
			CoverImageURL:     api.StringPtr(row.CoverImageUrl),
			Address:           api.StringPtr(row.Address),
			Neighborhood:      api.StringPtr(row.Neighborhood),
			City:              api.StringPtr(row.City),
			State:             api.StringPtr(row.State),
			Country:           api.StringPtr(row.Country),
			PostalCode:        api.StringPtr(row.PostalCode),
			Latitude:          api.DecimalPtr(row.Latitude),
			Longitude:         api.DecimalPtr(row.Longitude),
			PickupAvailable:   row.PickupAvailable,
			DeliveryAvailable: row.DeliveryAvailable,
			Timezone:          row.Timezone,
			LocationMode:      row.LocationMode,
			ServiceAreas:      business.MapServiceAreas(areaRows),
		},
		Hours: business.MapHourResponses(hourRows),
	})
}

type reachFilter struct {
	HasBBox, HasAreaFilter                                       bool
	MinLat, MaxLat, MinLng, MaxLng                               decimal.NullDecimal
	Country, State, Municipality, City, Neighborhood, PostalCode sql.NullString
	CountryKey, StateKey, MunicipalityKey, CityKey               sql.NullString
	NeighborhoodKey, PostalCodeKey                               sql.NullString
}

func parseReachFilter(c *fiber.Ctx) (reachFilter, error) {
	var filter reachFilter
	var err error
	if filter.MinLat, err = api.NullableDecimalQuery(c, "min_lat"); err != nil {
		return filter, err
	}
	if filter.MaxLat, err = api.NullableDecimalQuery(c, "max_lat"); err != nil {
		return filter, err
	}
	if filter.MinLng, err = api.NullableDecimalQuery(c, "min_lng"); err != nil {
		return filter, err
	}
	if filter.MaxLng, err = api.NullableDecimalQuery(c, "max_lng"); err != nil {
		return filter, err
	}
	filter.HasBBox = filter.MinLat.Valid || filter.MaxLat.Valid || filter.MinLng.Valid || filter.MaxLng.Valid
	for _, coordinate := range []struct {
		value decimal.NullDecimal
		name  string
		min   decimal.Decimal
		max   decimal.Decimal
	}{
		{filter.MinLat, "min_lat", decimal.NewFromInt(-90), decimal.NewFromInt(90)},
		{filter.MaxLat, "max_lat", decimal.NewFromInt(-90), decimal.NewFromInt(90)},
		{filter.MinLng, "min_lng", decimal.NewFromInt(-180), decimal.NewFromInt(180)},
		{filter.MaxLng, "max_lng", decimal.NewFromInt(-180), decimal.NewFromInt(180)},
	} {
		if coordinate.value.Valid {
			if err := v.DecimalRange(coordinate.value.Decimal, coordinate.name, coordinate.min, coordinate.max, 6); err != nil {
				return filter, err
			}
		}
	}
	if filter.MinLat.Valid && filter.MaxLat.Valid && filter.MinLat.Decimal.GreaterThan(filter.MaxLat.Decimal) {
		return filter, fiber.NewError(fiber.StatusBadRequest, "min_lat must be <= max_lat")
	}
	if filter.MinLng.Valid && filter.MaxLng.Valid && filter.MinLng.Decimal.GreaterThan(filter.MaxLng.Decimal) {
		return filter, fiber.NewError(fiber.StatusBadRequest, "min_lng must be <= max_lng")
	}

	type areaField struct {
		name string
		max  int
		raw  *sql.NullString
		key  *sql.NullString
	}
	fields := []areaField{
		{"country", 2, &filter.Country, &filter.CountryKey},
		{"state", 120, &filter.State, &filter.StateKey},
		{"municipality", 120, &filter.Municipality, &filter.MunicipalityKey},
		{"city", 120, &filter.City, &filter.CityKey},
		{"neighborhood", 120, &filter.Neighborhood, &filter.NeighborhoodKey},
		{"postal_code", 20, &filter.PostalCode, &filter.PostalCodeKey},
	}
	for _, field := range fields {
		value := v.Clean(c.Query(field.name))
		if value == "" {
			continue
		}
		if err := v.StringLength(value, field.name, 1, field.max); err != nil {
			return filter, err
		}
		if field.name == "country" {
			value = strings.ToUpper(value)
			if len(value) != 2 || value[0] < 'A' || value[0] > 'Z' || value[1] < 'A' || value[1] > 'Z' {
				return filter, fiber.NewError(fiber.StatusBadRequest, "country must be a two-letter code")
			}
		}
		if field.raw != nil {
			*field.raw = sql.NullString{String: value, Valid: true}
		}
		key := v.SearchKey(value)
		if field.name == "postal_code" {
			key = v.PostalKey(value)
		}
		*field.key = sql.NullString{String: key, Valid: key != ""}
		filter.HasAreaFilter = true
	}
	return filter, nil
}

func (h Handler) listPublicBusinesses(c *fiber.Ctx) error {
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	reach, err := parseReachFilter(c)
	if err != nil {
		return err
	}
	params := db.ListPublicBusinessesParams{
		HasBbox: reach.HasBBox, HasAreaFilter: reach.HasAreaFilter,
		MinLat: reach.MinLat, MaxLat: reach.MaxLat, MinLng: reach.MinLng, MaxLng: reach.MaxLng,
		Country: reach.Country, State: reach.State, Municipality: reach.Municipality, City: reach.City,
		Neighborhood: reach.Neighborhood, PostalCode: reach.PostalCode,
		CountryKey: reach.CountryKey, StateKey: reach.StateKey, MunicipalityKey: reach.MunicipalityKey,
		CityKey: reach.CityKey, NeighborhoodKey: reach.NeighborhoodKey, PostalCodeKey: reach.PostalCodeKey,
		OffsetCount: offset, LimitCount: limit,
	}
	rows, err := h.rt.Q.ListPublicBusinesses(c.Context(), params)
	if err != nil {
		return err
	}

	// Single batch query for every business on the page — avoids an N+1.
	businessIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		businessIDs = append(businessIDs, row.ID)
	}
	hoursByBusiness := map[uuid.UUID][]business.HourResponse{}
	areasByBusiness := map[uuid.UUID][]db.BusinessServiceArea{}
	if len(businessIDs) > 0 {
		hourRows, err := h.rt.Q.ListBusinessHoursForBusinesses(c.Context(), businessIDs)
		if err != nil {
			return err
		}
		hoursByBusiness = business.GroupHoursByBusiness(hourRows)
		areaRows, err := h.rt.Q.ListBusinessServiceAreasForBusinesses(c.Context(), businessIDs)
		if err != nil {
			return err
		}
		areasByBusiness = business.GroupServiceAreasByBusiness(areaRows)
	}

	out := make([]business.PublicResponse, 0, len(rows))
	for _, row := range rows {
		hours := hoursByBusiness[row.ID]
		if hours == nil {
			hours = []business.HourResponse{}
		}
		out = append(out, business.PublicResponse{
			Response: business.Response{
				ID:                row.ID,
				Name:              row.Name,
				Slug:              row.Slug,
				Description:       row.Description,
				BusinessType:      row.BusinessType,
				LogoURL:           api.StringPtr(row.LogoUrl),
				CoverImageURL:     api.StringPtr(row.CoverImageUrl),
				City:              api.StringPtr(row.City),
				State:             api.StringPtr(row.State),
				Country:           api.StringPtr(row.Country),
				Latitude:          api.DecimalPtr(row.Latitude),
				Longitude:         api.DecimalPtr(row.Longitude),
				PickupAvailable:   row.PickupAvailable,
				DeliveryAvailable: row.DeliveryAvailable,
				Timezone:          row.Timezone,
				LocationMode:      row.LocationMode,
				ServiceAreas:      business.MapServiceAreas(areasByBusiness[row.ID]),
			},
			Hours: hours,
		})
	}
	return c.JSON(out)
}

func (h Handler) searchMarketplaceProducts(c *fiber.Ctx) error {
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	query := v.Clean(c.Query("q"))
	if err := v.StringLength(query, "q", 0, 200); err != nil {
		return err
	}
	reach, err := parseReachFilter(c)
	if err != nil {
		return err
	}
	rows, err := h.rt.Q.SearchMarketplaceProducts(c.Context(), db.SearchMarketplaceProductsParams{
		SearchQuery: query, HasBbox: reach.HasBBox, HasAreaFilter: reach.HasAreaFilter,
		MinLat: reach.MinLat, MaxLat: reach.MaxLat, MinLng: reach.MinLng, MaxLng: reach.MaxLng,
		Country: reach.Country, State: reach.State, Municipality: reach.Municipality, City: reach.City,
		Neighborhood: reach.Neighborhood, PostalCode: reach.PostalCode,
		CountryKey: reach.CountryKey, StateKey: reach.StateKey, MunicipalityKey: reach.MunicipalityKey,
		CityKey: reach.CityKey, NeighborhoodKey: reach.NeighborhoodKey, PostalCodeKey: reach.PostalCodeKey,
		LimitCount: limit, OffsetCount: offset,
	})
	if err != nil {
		return err
	}
	return c.JSON(MapProducts(rows))
}
