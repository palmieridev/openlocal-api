package analytics

import (
	"errors"
	"github.com/palmieridev/openlocal-api/internal/analytics/analyticspostgres"
	"github.com/palmieridev/openlocal-api/internal/analytics/reporting"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	rt      api.Runtime
	service *reporting.Service
}

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt, service: reporting.New(analyticspostgres.New(rt.Q))}
}

func (h Handler) getABC(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	identity := reporting.Identity{BusinessID: businessID, ActorID: user.ID}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := h.service.ABC(c.Context(), identity, reporting.Page{Limit: limit, Offset: offset})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(abcResponses(items))
}

func (h Handler) getLowStock(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	identity := reporting.Identity{BusinessID: businessID, ActorID: user.ID}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := h.service.LowStock(c.Context(), identity, reporting.Page{Limit: limit, Offset: offset})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(lowStockResponses(items))
}

func (h Handler) getEOQ(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	identity := reporting.Identity{BusinessID: businessID, ActorID: user.ID}
	variantID, err := v.ParseUUID(c.Query("variant_id"), "variant_id")
	if err != nil {
		return err
	}
	periodDays, err := v.QueryInt32(c, "period_days", 90, 1, 730)
	if err != nil {
		return err
	}

	result, err := h.service.EOQ(c.Context(), identity, reporting.EOQInput{VariantID: variantID, PeriodDays: periodDays, OrderCost: c.Query("estimated_order_cost"), HoldingPercent: c.Query("estimated_holding_cost_percent")})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(eoqResponse(result))
}
func httpError(err error) error {
	var validation *reporting.ValidationError
	if errors.As(err, &validation) {
		return fiber.NewError(400, validation.Message)
	}
	if errors.Is(err, reporting.ErrNotFound) {
		return fiber.NewError(404, "not found")
	}
	return err
}
