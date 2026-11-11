package marketplace

import (
	"errors"
	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/marketplace/discovery"
	"github.com/palmieridev/openlocal-api/internal/marketplace/marketplacepostgres"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct{ service *discovery.Service }

func NewHandler(rt api.Runtime) Handler { return Handler{discovery.New(marketplacepostgres.New(rt.Q))} }
func httpError(err error) error {
	var validation *discovery.ValidationError
	if errors.As(err, &validation) {
		return fiber.NewError(400, validation.Message)
	}
	if errors.Is(err, discovery.ErrNotFound) {
		return fiber.NewError(404, "not found")
	}
	return err
}
func (h Handler) getPublicBusiness(c *fiber.Ctx) error {
	result, err := h.service.Business(c.Context(), c.Params("slug"))
	if err != nil {
		return httpError(err)
	}
	return c.JSON(businessResponse(result))
}
func filterInput(c *fiber.Ctx) (discovery.Input, error) {
	limit, offset, err := v.Page(c)
	if err != nil {
		return discovery.Input{}, err
	}
	return discovery.Input{Query: c.Query("q"), MinLat: c.Query("min_lat"), MaxLat: c.Query("max_lat"), MinLng: c.Query("min_lng"), MaxLng: c.Query("max_lng"), Country: c.Query("country"), State: c.Query("state"), Municipality: c.Query("municipality"), City: c.Query("city"), Neighborhood: c.Query("neighborhood"), PostalCode: c.Query("postal_code"), Limit: limit, Offset: offset}, nil
}
func (h Handler) listPublicBusinesses(c *fiber.Ctx) error {
	in, err := filterInput(c)
	if err != nil {
		return err
	}
	// q applies to product searches only; business listing historically ignores it.
	in.Query = ""
	rows, err := h.service.Businesses(c.Context(), in)
	if err != nil {
		return httpError(err)
	}
	out := make([]BusinessResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, businessResponse(row))
	}
	return c.JSON(out)
}
func (h Handler) searchMarketplaceProducts(c *fiber.Ctx) error {
	in, err := filterInput(c)
	if err != nil {
		return err
	}
	rows, err := h.service.Products(c.Context(), in)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(productResponses(rows))
}
