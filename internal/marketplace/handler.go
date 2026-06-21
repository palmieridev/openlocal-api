package marketplace

import (
	"database/sql"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/business"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
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
	row, err := h.rt.Q.GetPublicBusinessBySlug(c.Context(), v.Slug(c.Params("slug")))
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
	rows, err := h.rt.Q.SearchMarketplaceProducts(c.Context(), db.SearchMarketplaceProductsParams{
		Column1: v.Clean(c.Query("q")),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return err
	}
	return c.JSON(rows)
}
