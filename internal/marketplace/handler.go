package marketplace

import (
	"database/sql"

	"github.com/gofiber/fiber/v2"
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
	return c.JSON(business.Response{
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
		City:              row.City,
		State:             row.State,
		Country:           row.Country,
		PostalCode:        api.StringPtr(row.PostalCode),
		Latitude:          api.DecimalPtr(row.Latitude),
		Longitude:         api.DecimalPtr(row.Longitude),
		PickupAvailable:   row.PickupAvailable,
		DeliveryAvailable: row.DeliveryAvailable,
	})
}

func (h Handler) listPublicBusinesses(c *fiber.Ctx) error {
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	city := v.Clean(c.Query("city"))
	if err := v.StringLength(city, "city", 0, 120); err != nil {
		return err
	}
	params := db.ListPublicBusinessesParams{
		City:        sql.NullString{String: city, Valid: city != ""},
		OffsetCount: offset,
		LimitCount:  limit,
	}
	if params.MinLat, err = api.NullableDecimalQuery(c, "min_lat"); err != nil {
		return err
	}
	if params.MaxLat, err = api.NullableDecimalQuery(c, "max_lat"); err != nil {
		return err
	}
	if params.MinLng, err = api.NullableDecimalQuery(c, "min_lng"); err != nil {
		return err
	}
	if params.MaxLng, err = api.NullableDecimalQuery(c, "max_lng"); err != nil {
		return err
	}
	for _, coordinate := range []struct {
		value decimal.NullDecimal
		name  string
		min   decimal.Decimal
		max   decimal.Decimal
	}{
		{params.MinLat, "min_lat", decimal.NewFromInt(-90), decimal.NewFromInt(90)},
		{params.MaxLat, "max_lat", decimal.NewFromInt(-90), decimal.NewFromInt(90)},
		{params.MinLng, "min_lng", decimal.NewFromInt(-180), decimal.NewFromInt(180)},
		{params.MaxLng, "max_lng", decimal.NewFromInt(-180), decimal.NewFromInt(180)},
	} {
		if coordinate.value.Valid {
			if err := v.DecimalRange(coordinate.value.Decimal, coordinate.name, coordinate.min, coordinate.max, 6); err != nil {
				return err
			}
		}
	}
	if params.MinLat.Valid && params.MaxLat.Valid && params.MinLat.Decimal.GreaterThan(params.MaxLat.Decimal) {
		return fiber.NewError(fiber.StatusBadRequest, "min_lat must be <= max_lat")
	}
	if params.MinLng.Valid && params.MaxLng.Valid && params.MinLng.Decimal.GreaterThan(params.MaxLng.Decimal) {
		return fiber.NewError(fiber.StatusBadRequest, "min_lng must be <= max_lng")
	}
	rows, err := h.rt.Q.ListPublicBusinesses(c.Context(), params)
	if err != nil {
		return err
	}
	out := make([]business.Response, 0, len(rows))
	for _, row := range rows {
		out = append(out, business.Response{
			ID:                row.ID,
			Name:              row.Name,
			Slug:              row.Slug,
			Description:       row.Description,
			BusinessType:      row.BusinessType,
			LogoURL:           api.StringPtr(row.LogoUrl),
			CoverImageURL:     api.StringPtr(row.CoverImageUrl),
			City:              row.City,
			State:             row.State,
			Country:           row.Country,
			Latitude:          api.DecimalPtr(row.Latitude),
			Longitude:         api.DecimalPtr(row.Longitude),
			PickupAvailable:   row.PickupAvailable,
			DeliveryAvailable: row.DeliveryAvailable,
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
	rows, err := h.rt.Q.SearchMarketplaceProducts(c.Context(), db.SearchMarketplaceProductsParams{
		Column1: query,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return err
	}
	return c.JSON(MapProducts(rows))
}
